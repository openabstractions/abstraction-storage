package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	casapi "github.com/openabstractions/abstraction-cas/go/api"
	identity "github.com/openabstractions/abstraction-identity"
	"github.com/openabstractions/abstraction-identity/listen"
	storage "github.com/openabstractions/abstraction-storage/go"
	api "github.com/openabstractions/abstraction-storage/go/abstraction/storage/content"
)

var removalEndpointSequence atomic.Uint64

func TestArchiveWriterReceiptSurvivesIdleAndProvesPublishedFile(t *testing.T) {
	root := t.TempDir()
	store, err := storage.NewContentStore("runtime-content", root)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	path := filepath.Join(t.TempDir(), "writer-records.json")
	reopen := func() *writers {
		t.Helper()
		w, err := newWriters(store, permit, 1, casapi.BoundedFileStore{MaxBytes: MaxRecordFileBytes}, path, func() time.Time { return now })
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(w.close)
		return w
	}
	program := filepath.Join(root, "installed-panel.exe")
	native := nativeOwner{AccountKind: "windows", Account: "owner", Program: identity.NormalizeSubjectProgram(program), Executable: program}
	actor := ArchiveActor(native)
	body := []byte("abc")
	digest := named(body)
	id := "1234567890abcdef1234567890abcdef"
	check := func(context.Context, WriteCall) error { return nil }
	key := native.scope() + "\x00" + archiveWritePrefix + id
	w := reopen()
	begin := writeReceiver{writers: w, scope: native.scope(), ctx: t.Context()}
	if out, _ := begin.Begin(archiveWritePrefix+id, digest, int64(len(body))); out.Outcome != api.BeginOutcomeInvalid {
		t.Fatal("regular writer could use an archive identity")
	}
	captureFailure := errors.New("fixture staged identity capture failed")
	capture := archiveStagedKey
	t.Cleanup(func() { archiveStagedKey = capture })
	archiveStagedKey = func(context.Context, *os.File) (string, error) { return "", captureFailure }
	err = w.placeArchive(t.Context(), actor, id, digest, int64(len(body)), bytes.NewReader(body), check)
	archiveStagedKey = capture
	if !errors.Is(err, captureFailure) {
		t.Fatalf("staged key failure was lost: %v", err)
	}
	if _, found := store.Find(digest); found || w.records[key] != nil {
		t.Fatal("staged key failure published a blob or retained a receipt")
	}
	if err := w.placeArchive(t.Context(), actor, id, digest, int64(len(body)), bytes.NewReader(body), check); err != nil {
		t.Fatal(err)
	}
	if record := w.records[key]; record == nil || record.publishedFile == "" {
		t.Fatal("writer did not retain the native key from its published staging file")
	}
	w.close()
	now = now.Add(RequestRetention + time.Hour)
	w = reopen()
	w.sweep()
	if err := w.placeArchive(t.Context(), actor, id, digest, int64(len(body)), nil, check); err != nil {
		t.Fatalf("delayed restart lost the native receipt: %v", err)
	}
	ref, _ := store.Find(digest)
	blob := store.Path(ref)
	if err := os.WriteFile(blob, []byte("xyz"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := w.placeArchive(t.Context(), actor, id, digest, int64(len(body)), nil, check); err == nil {
		t.Fatal("delayed receipt accepted changed bytes in the original native file")
	}
	if err := os.Rename(blob, blob+".old"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(blob, body, 0600); err != nil {
		t.Fatal(err)
	}
	if err := w.placeArchive(t.Context(), actor, id, digest, int64(len(body)), nil, check); err == nil {
		t.Fatal("receipt accepted a same-size, same-digest replacement with a different native identity")
	}
	retained := w.records[key]
	if err := w.releaseArchive(actor, id, digest, int64(len(body))); err != nil {
		t.Fatal(err)
	}
	w.close()
	w = reopen()
	if w.records[key] != nil {
		t.Fatal("settled receipt was retained")
	}
	// An injected keyless receipt cannot settle even when the bytes match.
	retained.publishedFile = ""
	w.records[key] = retained
	if err := w.saveLocked(); err != nil {
		t.Fatal(err)
	}
	if err := w.placeArchive(t.Context(), actor, id, digest, int64(len(body)), nil, check); err == nil {
		t.Fatal("keyless in-memory receipt accepted matching bytes")
	}
	w.close()
	w = reopen()
	if err := w.placeArchive(t.Context(), actor, id, digest, int64(len(body)), nil, check); err == nil {
		t.Fatal("keyless disk receipt accepted matching bytes")
	}
	if w.records[key] != nil {
		t.Fatal("keyless receipt was retained")
	}
	if _, found := store.Find(digest); !found {
		t.Fatal("receipt release touched committed content")
	}
}

func TestRemovalCataloguePagesBeyondAnInventoryPage(t *testing.T) {
	store, err := storage.NewContentStore("runtime-content", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	const count = MaxInventoryPage + 1
	for i := 1; i <= count; i++ {
		leaf := filepath.Join(store.Root(), "blobs", fmt.Sprintf("sha256-%064x", i))
		if err := os.WriteFile(leaf, nil, 0600); err != nil {
			t.Fatal(err)
		}
	}
	owner := &removal{store: store, options: RemovalOptions{Gate: permit}}
	catalogue := removalSource{removalReceiver{removal: owner, scope: "native Panel scope", ctx: t.Context()}}
	first, err := catalogue.Snapshot("", MaxInventoryPage)
	if err != nil || first.Outcome != api.SourcePageOutcomePage || first.Complete || len(first.Objects) != MaxInventoryPage || first.Continuation == "" {
		t.Fatalf("first page: %s, %v, %d rows, %v", first.Outcome, first.Complete, len(first.Objects), err)
	}
	last, err := catalogue.Snapshot(first.Continuation, MaxInventoryPage)
	if err != nil || last.Outcome != api.SourcePageOutcomePage || !last.Complete || len(last.Objects) != 1 ||
		last.Continuation != "" || last.Objects[0].Object.Digest != fmt.Sprintf("sha256:%064x", count) {
		t.Fatalf("last page: %+v, %v", last, err)
	}
	catalogue.scope = "different native scope"
	refused, err := catalogue.Snapshot(first.Continuation, MaxInventoryPage)
	if err != nil || refused.Outcome != api.SourcePageOutcomeInvalid {
		t.Fatalf("a foreign scope used the cursor: %+v, %v", refused, err)
	}
}

func TestInlineRemovalTicketBindsIssuerAndExpiresUnderCustody(t *testing.T) {
	for _, expiry := range []string{"none", "ledger", "policy"} {
		t.Run(expiry, func(t *testing.T) {
			store, err := storage.NewContentStore("runtime-content", t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			ref, err := store.Place(digestA, 3)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(store.Path(ref), []byte("abc"), 0600); err != nil {
				t.Fatal(err)
			}
			if err := store.Commit(ref); err != nil {
				t.Fatal(err)
			}
			component, err := NewComponent(store, permit)
			if err != nil {
				t.Fatal(err)
			}
			defer component.Close()
			now := time.Date(2026, 11, 15, 12, 0, 0, 987654321, time.UTC)
			var deadline time.Time
			answered, completed := 0, 0
			opts := RemovalOptions{
				Gate: func(_ context.Context, _ *identity.Peer, resource string) error {
					if expiry == "policy" && answered == 1 && resource == "store:runtime-content" {
						now = deadline
					}
					return nil
				},
				Holds: permit, Sources: func(context.Context) []DesignatedSource {
					catalogue := &fakeSource{verify: sourcePage([]api.Store{{Name: "models", Placement: api.PlacementLocal}}, nil, nil, nil, "")}
					return []DesignatedSource{{Program: "inventoryd", Stores: []string{"models"}, Reader: catalogue}}
				},
				InlineAnswer: func(context.Context, *identity.Peer, string, string, string, string, int64) error {
					answered++
					if expiry == "ledger" {
						now = deadline
					}
					return nil
				},
				CompleteInline: func(context.Context, string) error { completed++; return nil },
				WithoutLease:   func(_ context.Context, _ string, action func() error) error { return action() },
				Records:        casapi.BoundedFileStore{MaxBytes: MaxRecordFileBytes}, RecordPath: filepath.Join(t.TempDir(), "holds.json"),
				Grace: time.Second,
			}
			if err := component.EnableRemoval(opts); err != nil {
				t.Fatal(err)
			}
			component.removals.now = func() time.Time { return now }
			receiver := func(name, account string) removalReceiver {
				path := filepath.Join(store.Root(), name+".exe")
				program := identity.NormalizeSubjectProgram(path)
				owner := nativeOwner{AccountKind: "windows", Account: account, Program: program, Executable: path}
				return removalReceiver{removal: component.removals, registry: component.registry, scope: owner.scope(),
					ctx: context.WithValue(t.Context(), nativeBindingKey{}, &nativeBinding{owner: owner})}
			}
			issuer := receiver("issuer-panel", "same-account")
			catalogue := removalSource{issuer}
			if _, err := catalogue.Verify(digestA); err != nil {
				t.Fatal(err)
			}
			now = now.Add(opts.Grace)
			page, err := catalogue.Verify(digestA)
			if err != nil || page.Outcome != api.SourcePageOutcomePage || len(page.Objects) != 1 {
				t.Fatalf("issue removal ticket: %+v %v", page, err)
			}
			ticket := page.Objects[0].Object.Locator
			deadline = now.Add(removalTicketLifetime)
			expect := func(c removalReceiver, want api.RemoveOutcome) {
				t.Helper()
				result, err := c.Remove(ticket)
				if err != nil || result.Outcome != want {
					t.Fatalf("Remove: %+v %v, want %s", result, err, want)
				}
			}
			expect(receiver("sibling-panel", "same-account"), api.RemoveOutcomeForbidden)
			expect(receiver("issuer-panel", "other-account"), api.RemoveOutcomeForbidden)
			if answered != 0 {
				t.Fatal("a different native recipient reached the answer ledger")
			}
			if expiry == "none" {
				expect(issuer, api.RemoveOutcomeRemoved)
				if answered != 1 || completed != 1 {
					t.Fatalf("issuer did not complete exactly once: %d, %d", answered, completed)
				}
				return
			}
			expect(issuer, api.RemoveOutcomeForbidden)
			if answered != 1 || completed != 0 {
				t.Fatalf("an expired answer settled an effect: %d, %d", answered, completed)
			}
			if _, ok := store.Find(digestA); !ok {
				t.Fatal("an expired answer removed the committed file")
			}
			if err := store.WalkRetiredRemovals(t.Context(), func(_, _ string) { t.Error("an expired answer created a retirement file") }); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestRemovalRequiresRightsTwoTruthfulObservationsAndAnExclusiveOpen(t *testing.T) {
	contentStore, err := storage.NewContentStore("runtime-content", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ref, err := contentStore.Place(digestA, 3)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(contentStore.Path(ref), []byte("abc"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := contentStore.Commit(ref); err != nil {
		t.Fatal(err)
	}
	ref, _ = contentStore.Find(digestA)
	index := store("ollama", "ollama")
	source := &fakeSource{verify: sourcePage([]api.Store{index}, nil, nil, nil, "")}
	c, err := NewComponent(contentStore, permit)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	granted := false
	answered := false
	lease := false
	var decided []string
	var removedSize int64
	options := RemovalOptions{
		Gate: func(_ context.Context, _ *identity.Peer, resource string) error {
			decided = append(decided, resource)
			if resource == "store:runtime-content" && !granted {
				return errors.New("exact store denied")
			}
			return nil
		},
		Holds: permit,
		Sources: func(context.Context) []DesignatedSource {
			return []DesignatedSource{{Program: "inventoryd", Stores: []string{"ollama"}, Reader: source}}
		},
		Observed: func(_ context.Context, _ *identity.Peer, resource, target string) error {
			if !answered || resource != "store:runtime-content" || target != digestA {
				return errors.New("no exact retained answer")
			}
			return nil
		},
		WithoutLease: func(_ context.Context, _ string, effect func() error) error {
			if lease {
				return storage.ErrRemovalBusy
			}
			return effect()
		},
		Records: casapi.BoundedFileStore{MaxBytes: MaxRecordFileBytes}, RecordPath: filepath.Join(t.TempDir(), "holds.json"),
		Grace: time.Second, Removed: func(_ string, size int64) { removedSize = size },
	}
	if err := c.EnableRemoval(options); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	c.removals.now = func() time.Time { return now }
	var remove removalReceiver
	for _, dispatcher := range c.Services(nil, t.Context()) {
		if d, ok := dispatcher.(*api.ContentRemoverDispatcher); ok {
			remove = d.Handler.(removalReceiver)
		}
	}
	check := func(want api.RemoveOutcome) api.RemoveResult {
		t.Helper()
		out, err := remove.Remove(digestA)
		if err != nil || out.Outcome != want {
			t.Fatalf("remove: %+v %v; want %s", out, err, want)
		}
		return out
	}
	check(api.RemoveOutcomeForbidden)
	remove.scope = "fixture proven Panel"
	check(api.RemoveOutcomeForbidden)
	if !slices.Equal(decided, []string{RemovalAccount, "store:runtime-content"}) {
		t.Fatalf("wrong gates: %v", decided)
	}
	granted = true
	m := namedManifest("ollama", digestB, "model", digestA)
	m.Holds = []api.Hold{observedHold("1", digestB, "claim")}
	source.verify.Manifests = []api.ManifestHolders{m}
	if got := check(api.RemoveOutcomeHeld); len(got.Holders) != 1 || got.Holders[0].Program != "lmstudio" {
		t.Fatalf("observed holder lost: %+v", got)
	}
	source.verify.Manifests = nil
	source.verify.Dangling = []api.Dangling{{Holder: api.Holder{Program: "ollama"}, Basis: api.Basis{Index: "models", Key: "want"}, ExpectedDigest: digestA}}
	check(api.RemoveOutcomeHeld)
	source.verify.Dangling = nil
	check(api.RemoveOutcomeUnavailable)
	now = now.Add(time.Second)
	source.verify.Stores[0].Errors = []api.StoreError{{Detail: storage.StaleObservationError}}
	check(api.RemoveOutcomeUnavailable)
	source.verify.Stores[0].Errors = nil
	check(api.RemoveOutcomeUnavailable)
	now = now.Add(time.Second)
	check(api.RemoveOutcomeForbidden)
	answered = true
	lease = true
	check(api.RemoveOutcomeBusy)
	lease = false
	open, err := os.Open(contentStore.Path(ref))
	if err != nil {
		t.Fatal(err)
	}
	defer open.Close()
	check(api.RemoveOutcomeBusy)
	if err := open.Close(); err != nil {
		t.Fatal(err)
	}
	check(api.RemoveOutcomeRemoved)
	if removedSize != 3 {
		t.Fatalf("activity event size = %d", removedSize)
	}
	check(api.RemoveOutcomeNotFound)
}

type blockingRemovalVerify struct {
	*fakeSource
	before func()
}

func (s *blockingRemovalVerify) Verify(target string) (api.SourcePage, error) {
	if s.before != nil {
		s.before()
	}
	return s.fakeSource.Verify(target)
}

func TestStorageRemovalFramedNativeHoldSurvivesReload(t *testing.T) {
	storeOwner, err := storage.NewContentStore("runtime-content", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ref, err := storeOwner.Place(digestA, 3)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(storeOwner.Path(ref), []byte("abc"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := storeOwner.Commit(ref); err != nil {
		t.Fatal(err)
	}
	source := &blockingRemovalVerify{fakeSource: &fakeSource{verify: sourcePage([]api.Store{store("ollama", "ollama")}, nil, nil, nil, "")}}
	authorize := func(_ context.Context, peer *identity.Peer, _ string) error {
		if peer == nil {
			return errors.New("unbound caller")
		}
		return nil
	}
	opts := RemovalOptions{Gate: authorize, Holds: authorize, Sources: func(context.Context) []DesignatedSource {
		return []DesignatedSource{{Program: "inventoryd", Stores: []string{"ollama"}, Reader: source}}
	}, Observed: func(context.Context, *identity.Peer, string, string) error { return errors.New("no confirmation") },
		WithoutLease: func(_ context.Context, _ string, effect func() error) error { return effect() },
		Records:      casapi.BoundedFileStore{MaxBytes: MaxRecordFileBytes}, RecordPath: filepath.Join(t.TempDir(), "holds.json"), Grace: 10 * time.Millisecond}
	run := func(f func(listen.FrameClient)) {
		t.Helper()
		endpoint := listen.Endpoint(fmt.Sprintf("storage-removal-%d-%d", os.Getpid(), removalEndpointSequence.Add(1)))
		h, err := Listen(endpoint, storeOwner, authorize)
		if err != nil {
			t.Fatal(err)
		}
		if err := h.EnableRemoval(opts); err != nil {
			_ = h.Close()
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan error, 1)
		go func() { done <- h.Serve(ctx) }()
		defer func() {
			cancel()
			_ = h.Close()
			if err := <-done; err != nil {
				t.Error(err)
			}
		}()
		f(listen.FrameClient{Endpoint: endpoint})
	}
	var holdID string
	run(func(transport listen.FrameClient) {
		client := api.NewHoldsClient(transport.WithContext(t.Context()))
		hold, err := client.Hold("hold-request-1234", digestA, "pinned", api.LifetimeUntilReleased, 0)
		if err != nil || hold.Outcome != api.HoldOutcomeHeld || hold.Hold == nil {
			t.Fatalf("hold: %+v %v", hold, err)
		}
		holdID = hold.Hold.ID
	})
	run(func(transport listen.FrameClient) {
		bound := transport.WithContext(t.Context())
		remove := api.NewContentRemoverClient(bound)
		held, err := remove.Remove(digestA)
		if err != nil || held.Outcome != api.RemoveOutcomeHeld || len(held.Holders) != 1 {
			t.Fatalf("retained hold: %+v %v", held, err)
		}
		released, err := api.NewHoldsClient(bound).Release(holdID)
		if err != nil || released.Outcome != api.ReleaseOutcomeReleased {
			t.Fatalf("release: %+v %v", released, err)
		}
		inventory := api.NewInventorySourceClient(bound)
		page, err := inventory.Snapshot("", 256)
		if err != nil || page.Outcome != api.SourcePageOutcomePage || len(page.Objects) != 1 || page.Objects[0].Object.Digest != digestA {
			t.Fatalf("private snapshot: %+v %v", page, err)
		}
		before, err := inventory.Verify(digestA)
		if err != nil || before.Outcome != api.SourcePageOutcomeUnavailable {
			t.Fatalf("first verification: %+v %v", before, err)
		}
		time.Sleep(20 * time.Millisecond)
		ready, err := inventory.Verify(digestA)
		if err != nil || ready.Outcome != api.SourcePageOutcomePage || len(ready.Objects) != 1 {
			t.Fatalf("second verification: %+v %v", ready, err)
		}
		if _, found := storeOwner.Find(digestA); !found {
			t.Fatal("Verify removed content")
		}
		entered, proceed := make(chan struct{}), make(chan struct{})
		source.before = func() {
			close(entered)
			<-proceed
		}
		waiting := make(chan api.RemoveOutcome, 1)
		go func() {
			out, err := remove.Remove(digestA)
			if err != nil {
				t.Error(err)
			}
			waiting <- out.Outcome
		}()
		select {
		case <-entered:
		case <-time.After(time.Second):
			close(proceed)
			t.Fatal("Remove never started its final Verify")
		}
		holds := api.NewHoldsClient(bound)
		racing, err := holds.Hold("racing-hold-request", digestA, "pinned", api.LifetimeUntilReleased, 0)
		close(proceed)
		if err != nil || racing.Outcome != api.HoldOutcomeHeld || racing.Hold == nil {
			t.Fatalf("racing Hold: %+v %v", racing, err)
		}
		if outcome := <-waiting; outcome != api.RemoveOutcomeHeld {
			t.Fatalf("racing Remove: %s", outcome)
		}
		source.before = nil
		if _, found := storeOwner.Find(digestA); !found {
			t.Fatal("racing hold lost its content")
		}
		if out, err := holds.Release(racing.Hold.ID); err != nil || out.Outcome != api.ReleaseOutcomeReleased {
			t.Fatalf("release racing Hold: %+v %v", out, err)
		}
		if initial, err := inventory.Verify(digestA); err != nil || initial.Outcome != api.SourcePageOutcomeUnavailable {
			t.Fatalf("invalidated proof survived hold change: %+v %v", initial, err)
		}
		time.Sleep(20 * time.Millisecond)
		out, err := remove.Remove(digestA)
		if err != nil || out.Outcome != api.RemoveOutcomeRemoved {
			t.Fatalf("remove: %+v %v", out, err)
		}
	})
}
