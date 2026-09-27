package service

import (
	"context"
	"errors"
	"testing"

	identity "github.com/openabstractions/abstraction-identity"
	api "github.com/openabstractions/abstraction-storage/go/abstraction/storage/content"
)

const (
	digestA = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	digestB = "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	digestF = "sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"
)

// fakeSource answers Snapshot from fixed pages and Verify from one page.
type fakeSource struct {
	pages    []api.SourcePage
	verify   api.SourcePage
	err      error
	requests []string
}

func (f *fakeSource) Snapshot(continuation string, limit int64) (api.SourcePage, error) {
	f.requests = append(f.requests, continuation)
	if f.err != nil {
		return api.SourcePage{}, f.err
	}
	if continuation == "" {
		return f.pages[0], nil
	}
	for i := 1; i < len(f.pages); i++ {
		if f.pages[i-1].Continuation == continuation {
			return f.pages[i], nil
		}
	}
	return api.SourcePage{Outcome: api.SourcePageOutcomeGap}, nil
}

func (f *fakeSource) Verify(target string) (api.SourcePage, error) {
	if f.err != nil {
		return api.SourcePage{}, f.err
	}
	return f.verify, nil
}

func namedManifest(store, id, name string, digests ...string) api.ManifestHolders {
	m := api.Manifest{ID: id, Addressing: api.AddressingName, Kind: "abstraction.model/descriptor@1", Store: store,
		Names: []api.Name{{Scheme: store, Name: name}}, Entries: []api.Entry{}}
	for _, d := range digests {
		m.Entries = append(m.Entries, api.Entry{Role: "weights", Digest: d, Size: 8, MediaType: "application/octet-stream",
			Evidence: api.EvidenceNamed, Locator: name})
	}
	if len(digests) == 0 {
		m.Entries = append(m.Entries, api.Entry{Role: "weights", Size: 8, MediaType: "application/octet-stream",
			Evidence: api.EvidenceNone, Locator: name})
	}
	return api.ManifestHolders{Manifest: m, Holds: []api.Hold{}}
}

func observedHold(id, target, establishedBy string) api.Hold {
	return api.Hold{ID: id, Target: target, Purpose: "installed", Lifetime: api.LifetimeWhilePresent,
		Attestation: api.AttestationObserved, Basis: &api.Basis{Index: "index", Key: "key"},
		Holder: api.Holder{Account: "owner", Program: "lmstudio", Instance: "one", EstablishedBy: establishedBy}}
}

func strayObject(store, locator, digest string) api.ObjectHolders {
	evidence := api.EvidenceNone
	if digest != "" {
		evidence = api.EvidenceNamed
	}
	return api.ObjectHolders{Store: store, Holds: []api.Hold{},
		Object: api.Object{Locator: locator, Size: 16, Digest: digest, Evidence: evidence}}
}

func sourcePage(stores []api.Store, manifests []api.ManifestHolders, objects []api.ObjectHolders, dangling []api.Dangling, continuation string) api.SourcePage {
	return api.SourcePage{Outcome: api.SourcePageOutcomePage, Stores: stores, Manifests: manifests, Objects: objects,
		Dangling: dangling, Changes: []api.SourceChange{}, Continuation: continuation, Complete: continuation == ""}
}

func store(name, program string, errs ...api.StoreError) api.Store {
	if errs == nil {
		errs = []api.StoreError{}
	}
	return api.Store{Name: name, Program: program, Locator: "opaque:" + name, Rule: "a default location",
		Present: true, Errors: errs, Placement: api.PlacementLocal}
}

func permit(context.Context, *identity.Peer, string) error { return nil }

func composedReceiver(sources ...DesignatedSource) inventoryReceiver {
	return receiverWith(permit, permit, sources...)
}

func receiverWith(read, gate Policy, sources ...DesignatedSource) inventoryReceiver {
	list := sources
	i := newInventory(read, gate, func(context.Context) []DesignatedSource { return list }, func() string { return "epoch:7" })
	return inventoryReceiver{inventory: i, scope: "test-scope", ctx: context.Background()}
}

// The composition carries every accepted store on the first page, then the
// manifests, stray objects and dangling references, and drops a store the
// runtime never accepted.
func TestComposedInventoryPagesStoresThenRecords(t *testing.T) {
	source := &fakeSource{pages: []api.SourcePage{sourcePage(
		[]api.Store{store("lmstudio", "lmstudio"), store("ollama", "ollama"), store("unaccepted", "other")},
		[]api.ManifestHolders{namedManifest("lmstudio", "m1", "a/one.gguf"), namedManifest("unaccepted", "m2", "b/two.gguf")},
		[]api.ObjectHolders{strayObject("ollama", "blob-1", ""), strayObject("unaccepted", "blob-2", "")},
		[]api.Dangling{{Holder: api.Holder{Program: "lemonade", EstablishedBy: "somebody-else"},
			Basis: api.Basis{Index: "i", Key: "k"}, Reference: "wanted"}}, "")}}
	c := composedReceiver(DesignatedSource{Program: "/bin/inventoryd", Stores: []string{"lmstudio", "ollama"}, Reader: source})

	page, err := c.List("", 2)
	if err != nil {
		t.Fatal(err)
	}
	if page.Outcome != api.InventoryOutcomePage || len(page.Stores) != 2 ||
		page.Stores[0].Name != "lmstudio" || page.Stores[1].Name != "ollama" {
		t.Fatalf("first page stores %+v", page.Stores)
	}
	if page.Cursor != "epoch:7" {
		t.Fatalf("cursor %q", page.Cursor)
	}
	if len(page.Manifests) != 1 || page.Manifests[0].Manifest.ID != "m1" {
		t.Fatalf("manifests %+v", page.Manifests)
	}
	if len(page.Objects) != 1 || page.Objects[0].Object.Locator != "blob-1" {
		t.Fatalf("objects %+v", page.Objects)
	}
	if page.Complete || page.Continuation == "" {
		t.Fatalf("expected a second page: %+v", page)
	}

	second, err := c.List(page.Continuation, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Stores) != 0 {
		t.Fatalf("stores repeated: %+v", second.Stores)
	}
	if len(second.Dangling) != 1 || second.Dangling[0].Holder.EstablishedBy != "/bin/inventoryd" {
		t.Fatalf("dangling %+v", second.Dangling)
	}
	if !second.Complete || second.Continuation != "" {
		t.Fatalf("expected the last page: %+v", second)
	}
}

// A store the source could not read entirely reports its errors on its own
// Store record, and everything else it read is still listed.
func TestOneStoreErrorTravelsOnItsStoreRecord(t *testing.T) {
	failing := store("comfyui", "comfyui", api.StoreError{Kind: api.StoreErrorKindUnreadableTree, Locator: "opaque:comfyui", Detail: "denied"})
	source := &fakeSource{pages: []api.SourcePage{sourcePage(
		[]api.Store{failing, store("lmstudio", "lmstudio")},
		[]api.ManifestHolders{namedManifest("lmstudio", "m1", "a/one.gguf")},
		[]api.ObjectHolders{}, []api.Dangling{}, "")}}
	c := composedReceiver(DesignatedSource{Program: "/bin/inventoryd", Stores: []string{"comfyui", "lmstudio"}, Reader: source})

	page, err := c.List("", 8)
	if err != nil {
		t.Fatal(err)
	}
	if page.Outcome != api.InventoryOutcomePage || len(page.Stores) != 2 {
		t.Fatalf("page %+v", page)
	}
	if len(page.Stores[0].Errors) != 1 || page.Stores[0].Errors[0].Detail != "denied" {
		t.Fatalf("store errors %+v", page.Stores[0])
	}
	if len(page.Manifests) != 1 || !page.Complete {
		t.Fatalf("records %+v", page)
	}
}

// A source that does not answer refuses the whole composition: an inventory
// that dropped it would report an absence it cannot see.
func TestASourceThatDoesNotAnswerRefusesThePage(t *testing.T) {
	source := &fakeSource{err: errors.New("pipe closed")}
	c := composedReceiver(DesignatedSource{Program: "/bin/inventoryd", Stores: []string{"lmstudio"}, Reader: source})
	page, err := c.List("", 8)
	if err != nil {
		t.Fatal(err)
	}
	if page.Outcome != api.InventoryOutcomeUnavailable || len(page.Stores) != 0 || page.Complete || page.Cursor != "" {
		t.Fatalf("page %+v", page)
	}
}

// Every hold a source reports is an observation established by the bound
// source program; a hold spelled otherwise is not carried.
func TestHoldsAreRewrittenToTheBoundSourceProgram(t *testing.T) {
	manifest := namedManifest("lmstudio", "m1", "a/one.gguf")
	manifest.Holds = []api.Hold{observedHold("h1", "m1", "a-claim-of-its-own"),
		{ID: "h2", Target: "m1", Purpose: "installed", Lifetime: api.LifetimeUntilReleased, Attestation: api.AttestationDeclared}}
	source := &fakeSource{pages: []api.SourcePage{sourcePage([]api.Store{store("lmstudio", "lmstudio")},
		[]api.ManifestHolders{manifest}, []api.ObjectHolders{}, []api.Dangling{}, "")}}
	c := composedReceiver(DesignatedSource{Program: "/bin/inventoryd", Stores: []string{"lmstudio"}, Reader: source})

	page, err := c.List("", 8)
	if err != nil {
		t.Fatal(err)
	}
	holds := page.Manifests[0].Holds
	if len(holds) != 1 || holds[0].ID != "h1" || holds[0].Holder.EstablishedBy != "/bin/inventoryd" {
		t.Fatalf("holds %+v", holds)
	}
}

// A record the read policy refuses for one of its digests is omitted without
// a count; a record carrying no digest has nothing to decide.
func TestRecordsAreFilteredByTheirDigests(t *testing.T) {
	read := func(_ context.Context, _ *identity.Peer, resource string) error {
		if resource == digestF {
			return errors.New("denied")
		}
		return nil
	}
	source := &fakeSource{pages: []api.SourcePage{sourcePage([]api.Store{store("lmstudio", "lmstudio")},
		[]api.ManifestHolders{namedManifest("lmstudio", "m1", "a/one.gguf", digestF),
			namedManifest("lmstudio", "m2", "b/two.gguf", digestA),
			namedManifest("lmstudio", "m3", "c/three.gguf")},
		[]api.ObjectHolders{}, []api.Dangling{}, "")}}
	c := receiverWith(read, permit, DesignatedSource{Program: "/bin/inventoryd", Stores: []string{"lmstudio"}, Reader: source})

	page, err := c.List("", 8)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Manifests) != 2 || page.Manifests[0].Manifest.ID != "m2" || page.Manifests[1].Manifest.ID != "m3" {
		t.Fatalf("manifests %+v", page.Manifests)
	}
	if !page.Complete {
		t.Fatalf("page %+v", page)
	}
}

// The gate refuses the whole call, and a refusal carries nothing.
func TestTheInventoryGateRefusesWithEmptyLists(t *testing.T) {
	source := &fakeSource{pages: []api.SourcePage{sourcePage([]api.Store{store("lmstudio", "lmstudio")},
		[]api.ManifestHolders{namedManifest("lmstudio", "m1", "a/one.gguf")}, []api.ObjectHolders{}, []api.Dangling{}, "")}}
	denied := func(context.Context, *identity.Peer, string) error { return errors.New("no rule") }
	c := receiverWith(permit, denied, DesignatedSource{Program: "/bin/inventoryd", Stores: []string{"lmstudio"}, Reader: source})

	for _, page := range []api.InventoryPage{must(c.List("", 8)), must(c.Find(digestA)), must(c.Holders("m1"))} {
		if page.Outcome != api.InventoryOutcomeForbidden || len(page.Manifests) != 0 || len(page.Stores) != 0 ||
			page.Continuation != "" || page.Cursor != "" || page.Complete {
			t.Fatalf("refusal %+v", page)
		}
	}
	if len(source.requests) != 0 {
		t.Fatalf("a refused call read a source: %v", source.requests)
	}
}

// Find answers from a composition taken now; Holders re-reads through Verify
// and returns what the source observed as verified.
func TestFindAndHoldersReadTheSourceNow(t *testing.T) {
	page := sourcePage([]api.Store{store("lmstudio", "lmstudio")},
		[]api.ManifestHolders{namedManifest("lmstudio", "m1", "a/one.gguf", digestA)},
		[]api.ObjectHolders{strayObject("lmstudio", "blob-1", digestB)}, []api.Dangling{}, "")
	verified := namedManifest("lmstudio", "m1", "a/one.gguf", digestA)
	verified.Holds = []api.Hold{observedHold("h1", "m1", "elsewhere")}
	source := &fakeSource{pages: []api.SourcePage{page},
		verify: sourcePage([]api.Store{}, []api.ManifestHolders{verified}, []api.ObjectHolders{}, []api.Dangling{}, "")}
	c := composedReceiver(DesignatedSource{Program: "/bin/inventoryd", Stores: []string{"lmstudio"}, Reader: source})

	found := must(c.Find(digestB))
	if found.Outcome != api.InventoryOutcomePage || len(found.Objects) != 1 || len(found.Manifests) != 0 || !found.Complete {
		t.Fatalf("find %+v", found)
	}
	holders := must(c.Holders("m1"))
	if len(holders.Manifests) != 1 || len(holders.Manifests[0].Holds) != 1 ||
		holders.Manifests[0].Holds[0].Attestation != api.AttestationVerified ||
		holders.Manifests[0].Holds[0].Holder.EstablishedBy != "/bin/inventoryd" {
		t.Fatalf("holders %+v", holders)
	}
}

// Unheld needs two observations grace_ms apart and a hold table; this
// composition keeps neither and says so.
func TestUnheldIsUnavailableWithoutASecondObservation(t *testing.T) {
	source := &fakeSource{pages: []api.SourcePage{sourcePage([]api.Store{store("lmstudio", "lmstudio")},
		[]api.ManifestHolders{}, []api.ObjectHolders{}, []api.Dangling{}, "")}}
	c := composedReceiver(DesignatedSource{Program: "/bin/inventoryd", Stores: []string{"lmstudio"}, Reader: source})
	page := must(c.Unheld("", 8))
	if page.Outcome != api.InventoryOutcomeUnavailable || page.GraceMs != 0 || page.AuditRetentionMs != 0 {
		t.Fatalf("unheld %+v", page)
	}
	if bad := must(c.Unheld("", 0)); bad.Outcome != api.InventoryOutcomeInvalid {
		t.Fatalf("bounds %+v", bad)
	}
}

// A continuation of another caller's frozen composition is forbidden, and an
// unknown one is a gap.
func TestAnotherScopesContinuationIsRefused(t *testing.T) {
	source := &fakeSource{pages: []api.SourcePage{sourcePage([]api.Store{store("lmstudio", "lmstudio")},
		[]api.ManifestHolders{namedManifest("lmstudio", "m1", "a/one.gguf"), namedManifest("lmstudio", "m2", "b/two.gguf")},
		[]api.ObjectHolders{}, []api.Dangling{}, "")}}
	c := composedReceiver(DesignatedSource{Program: "/bin/inventoryd", Stores: []string{"lmstudio"}, Reader: source})
	first := must(c.List("", 1))
	other := c
	other.scope = "another-scope"
	if page := must(other.List(first.Continuation, 1)); page.Outcome != api.InventoryOutcomeForbidden {
		t.Fatalf("other scope %+v", page)
	}
	if page := must(c.List("unknown:0", 1)); page.Outcome != api.InventoryOutcomeGap {
		t.Fatalf("unknown continuation %+v", page)
	}
	if page := must(c.List("no-offset", 1)); page.Outcome != api.InventoryOutcomeInvalid {
		t.Fatalf("malformed continuation %+v", page)
	}
}

func must(page api.InventoryPage, err error) api.InventoryPage {
	if err != nil {
		panic(err)
	}
	return page
}
