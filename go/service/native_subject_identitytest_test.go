//go:build oa_identitytest

package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	cas "github.com/openabstractions/abstraction-cas/go"
	casapi "github.com/openabstractions/abstraction-cas/go/api"
	identity "github.com/openabstractions/abstraction-identity"
	"github.com/openabstractions/abstraction-identity/identitytest"
	api "github.com/openabstractions/abstraction-storage/go/abstraction/storage/content"
)

type nativeWriterCase struct {
	peer    *identity.Peer
	binding *nativeBinding
}

func nativeWriterCases(t *testing.T) []nativeWriterCase {
	t.Helper()
	root := t.TempDir()
	var facts []identitytest.Facts
	for i, version := range []string{"1.0.0.0", "2.0.0.0", "3.0.0.0"} {
		folder := filepath.Join(root, version)
		if err := os.MkdirAll(folder, 0700); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(folder, "app.exe")
		if err := os.WriteFile(path, []byte("inert fixture"), 0600); err != nil {
			t.Fatal(err)
		}
		name := "SubjectFixture"
		if i == 2 {
			name = "OtherFixture"
		}
		facts = append(facts, identitytest.Facts{User: identity.User{Kind: "windows", SID: "owner"}, Path: path, Package: name + "_" + version + "_x64__pzs8sxrjxfjjc", PackageRoot: folder})
	}
	var out []nativeWriterCase
	for _, peer := range identitytest.New(t, facts...) {
		binding := receivingNativeBinding(peer, "owner")
		if binding == nil {
			t.Fatal("proven fixture has no binding")
		}
		out = append(out, nativeWriterCase{peer, binding})
	}
	if out[0].binding.owner.scope() != out[1].binding.owner.scope() || out[0].binding.legacy == out[1].binding.legacy {
		t.Fatal("fixture did not separate canonical and exact legacy namespaces")
	}
	return out
}

func (n nativeWriterCase) receiver(base writeReceiver) writeReceiver {
	base.scope = n.binding.owner.scope()
	base.peer = n.peer
	base.ctx = context.WithValue(context.Background(), nativeBindingKey{}, n.binding)
	return base
}

func TestNativeWriterCanonicalReplaySurvivesVersionAndRestart(t *testing.T) {
	n := nativeWriterCases(t)
	k := newWriterCase(t, 1<<20)
	body := []byte("canonical content")
	request := strings.Repeat("a", 32)
	k.c = n[0].receiver(k.c)
	first, _ := k.c.Begin(request, named(body), int64(len(body)))
	if first.Outcome != api.BeginOutcomeStarted {
		t.Fatal(first)
	}
	k.put(t, first.Upload.Handle, body, 0)
	done, _ := k.c.Commit(first.Upload.Handle)
	if done.Outcome != api.CommitOutcomeCommitted {
		t.Fatal(done)
	}
	k.restart(t, time.Now)
	k.c = n[1].receiver(k.c)
	finds, places, commits := k.store.finds.Load(), k.store.places.Load(), k.store.commits.Load()
	replay, _ := k.c.Begin(request, named(body), int64(len(body)))
	if replay.Outcome != api.BeginOutcomeCommitted || *replay.Stored != *done.Stored {
		t.Fatal(replay, done)
	}
	changed := []byte("changed intent")
	conflict, _ := k.c.Begin(request, named(changed), int64(len(changed)))
	if conflict.Outcome != api.BeginOutcomeConflict {
		t.Fatal(conflict)
	}
	if k.store.finds.Load() != finds || k.store.places.Load() != places || k.store.commits.Load() != commits {
		t.Fatal("replay/conflict reached provider")
	}
	rec := k.w.records[k.c.scope+"\x00"+request]
	if rec == nil || rec.native == nil || rec.native.Executable != n[0].binding.owner.Executable || rec.native.Program != n[1].binding.owner.Program {
		t.Fatal("captured provenance changed", rec)
	}
}

func TestNativeWriterExactLegacyReplayDoesNotPromoteOldRecord(t *testing.T) {
	n := nativeWriterCases(t)
	k := newWriterCase(t, 1<<20)
	body := []byte("legacy content")
	request := strings.Repeat("l", 32)
	k.c.scope = n[0].binding.legacy
	first, _ := k.c.Begin(request, named(body), int64(len(body)))
	k.put(t, first.Upload.Handle, body, 0)
	done, _ := k.c.Commit(first.Upload.Handle)
	raw, err := os.ReadFile(k.records)
	if err != nil {
		t.Fatal(err)
	}
	raw = bytes.Replace(raw, []byte(RecordProfile), []byte(legacyRecordProfile), 1)
	if err := os.WriteFile(k.records, raw, 0600); err != nil {
		t.Fatal(err)
	}
	k.restart(t, time.Now)
	k.c = n[0].receiver(k.c)
	replay, _ := k.c.Begin(request, named(body), int64(len(body)))
	if replay.Outcome != api.BeginOutcomeCommitted || *replay.Stored != *done.Stored {
		t.Fatal(replay, done)
	}
	if rec := k.w.records[n[0].binding.legacy+"\x00"+request]; rec == nil || rec.native != nil || len(k.w.records) != 1 {
		t.Fatal("legacy row relabeled", rec)
	}
	after, _ := os.ReadFile(k.records)
	if !bytes.Equal(raw, after) {
		t.Fatal("legacy replay rewrote old bytes")
	}
	other := []byte("different bytes")
	refused, _ := k.c.Begin(request, named(other), int64(len(other)))
	if refused.Outcome != api.BeginOutcomeConflict {
		t.Fatal(refused)
	}
	// Another package version cannot receive an unproven old result.
	k.c = n[1].receiver(k.c)
	fresh, _ := k.c.Begin(request, named(other), int64(len(other)))
	if fresh.Outcome != api.BeginOutcomeStarted {
		t.Fatal("old result transferred across versions", fresh)
	}
	if k.w.records[n[0].binding.legacy+"\x00"+request].native != nil {
		t.Fatal("old row gained guessed provenance")
	}
}

func TestNativeWriterConcurrentVersionAliasesReserveOneUpload(t *testing.T) {
	n := nativeWriterCases(t)
	k := newWriterCase(t, 1<<20)
	body := []byte("one upload")
	request := strings.Repeat("c", 32)
	var results [2]api.BeginResult
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := range results {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			results[i], _ = n[i].receiver(k.c).Begin(request, named(body), int64(len(body)))
		}(i)
	}
	close(start)
	wg.Wait()
	handle := ""
	for _, result := range results {
		if result.Outcome == api.BeginOutcomeBusy {
			continue
		}
		if result.Outcome != api.BeginOutcomeStarted {
			t.Fatal(result)
		}
		if handle != "" && handle != result.Upload.Handle {
			t.Fatal("two accepted uploads", results)
		}
		handle = result.Upload.Handle
	}
	if handle == "" || len(k.w.records) != 1 || len(k.w.uploads) != 1 || k.store.places.Load() != 1 {
		t.Fatal("duplicate reservation", results)
	}
	k.c = n[1].receiver(k.c)
	k.put(t, handle, body, 0)
	if result, _ := k.c.Commit(handle); result.Outcome != api.CommitOutcomeCommitted || k.store.commits.Load() != 1 {
		t.Fatal(result)
	}
}

func TestNativeWriterLegacyUploadHandleRetainsItsOwner(t *testing.T) {
	n := nativeWriterCases(t)
	k := newWriterCase(t, 1<<20)
	body := []byte("legacy live upload")
	request := strings.Repeat("u", 32)
	k.c.scope = n[0].binding.legacy
	first, _ := k.c.Begin(request, named(body), int64(len(body)))
	k.c = n[0].receiver(k.c)
	replay, _ := k.c.Begin(request, named(body), int64(len(body)))
	if replay.Outcome != api.BeginOutcomeStarted || replay.Upload.Handle != first.Upload.Handle {
		t.Fatal(replay)
	}
	foreign := n[1].receiver(k.c)
	if denied, _ := foreign.Append(first.Upload.Handle, 0, body); denied.Outcome != api.AppendOutcomeForbidden {
		t.Fatal("legacy handle transferred", denied)
	}
	k.put(t, first.Upload.Handle, body, 0)
	if done, _ := k.c.Commit(first.Upload.Handle); done.Outcome != api.CommitOutcomeCommitted {
		t.Fatal(done)
	}
	if k.w.records[n[0].binding.legacy+"\x00"+request].native != nil {
		t.Fatal("legacy live record promoted")
	}
}

func TestNativeWriterDualAcceptedRowsRefuseWithoutChoosing(t *testing.T) {
	n := nativeWriterCases(t)
	k := newWriterCase(t, 1<<20)
	body := []byte("stored once")
	digest := named(body)
	request := strings.Repeat("d", 32)
	first, _ := k.c.Begin(strings.Repeat("s", 32), digest, int64(len(body)))
	k.put(t, first.Upload.Handle, body, 0)
	k.c.Commit(first.Upload.Handle)
	owner := n[0].binding.owner
	file := recordFile{Profile: RecordProfile, Records: []recordEntry{
		{Scope: owner.scope(), Request: request, Digest: digest, Size: int64(len(body)), StoredSize: int64(len(body)), Evidence: "hashed", Expires: time.Now().Add(RequestRetention).UnixMilli(), Native: &owner},
		{Scope: n[0].binding.legacy, Request: request, Digest: digest, Size: int64(len(body)), StoredSize: int64(len(body)), Evidence: "hashed", Expires: time.Now().Add(RequestRetention).UnixMilli()},
	}}
	raw, _ := json.Marshal(file)
	if err := os.WriteFile(k.records, raw, 0600); err != nil {
		t.Fatal(err)
	}
	k.restart(t, time.Now)
	k.c = n[0].receiver(k.c)
	finds, places, commits := k.store.finds.Load(), k.store.places.Load(), k.store.commits.Load()
	result, _ := k.c.Begin(request, digest, int64(len(body)))
	if result.Outcome != api.BeginOutcomeConflict || len(k.w.records) != 2 {
		t.Fatal(result)
	}
	if k.store.finds.Load() != finds || k.store.places.Load() != places || k.store.commits.Load() != commits {
		t.Fatal("ambiguous replay reached provider")
	}
}

func TestNativeWriterCASReplacementFencesBeforeStagingAndReplay(t *testing.T) {
	n := nativeWriterCases(t)
	k := newWriterCase(t, 1<<20)
	body := []byte("fenced content")
	request := strings.Repeat("f", 32)
	k.c = n[0].receiver(k.c)
	first, _ := k.c.Begin(request, named(body), int64(len(body)))
	k.put(t, first.Upload.Handle, body, 0)
	k.c.Commit(first.Upload.Handle)
	raw, _ := os.ReadFile(k.records)
	if err := os.WriteFile(k.records, []byte{}, 0600); err != nil {
		t.Fatal(err)
	}
	places, finds := k.store.places.Load(), k.store.finds.Load()
	for _, key := range []string{request, strings.Repeat("n", 32)} {
		if result, _ := k.c.Begin(key, named(body), int64(len(body))); result.Outcome != api.BeginOutcomeUnavailable {
			t.Fatal(result)
		}
	}
	if err := os.WriteFile(k.records, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if result, _ := k.c.Begin(request, named(body), int64(len(body))); result.Outcome != api.BeginOutcomeUnavailable {
		t.Fatal("observed fence was cleared", result)
	}
	if k.store.places.Load() != places || k.store.finds.Load() != finds {
		t.Fatal("record replacement reached provider")
	}
}

func TestNativeWriterProfileValidationAndLegacyEquation(t *testing.T) {
	n := nativeWriterCases(t)
	k := newWriterCase(t, 1<<20)
	var legacy []string
	if err := json.Unmarshal([]byte(n[0].binding.legacy), &legacy); err != nil {
		t.Fatal(err)
	}
	want, _ := json.Marshal([]string{"owner-program@1", "windows", "owner", n[0].binding.owner.Executable})
	if n[0].binding.legacy != string(want) || len(legacy) != 4 {
		t.Fatal("legacy equation changed", legacy)
	}
	row := recordEntry{Scope: n[0].binding.owner.scope(), Request: strings.Repeat("p", 32), Digest: named([]byte("x")), Size: 1, Expires: time.Now().Add(RequestRetention).UnixMilli(), Native: &n[0].binding.owner}
	for _, variant := range []string{"legacy-metadata", "wrong-provenance", "unknown-profile", "trailing"} {
		file := recordFile{Profile: RecordProfile, Records: []recordEntry{row}}
		switch variant {
		case "legacy-metadata":
			file.Profile = legacyRecordProfile
		case "wrong-provenance":
			file.Records[0].Scope = "wrong"
		case "unknown-profile":
			file.Profile = "abstraction.storage/writer-records@3"
		}
		raw, _ := json.Marshal(file)
		if variant == "trailing" {
			raw = append(raw, []byte(" {}")...)
		}
		if err := os.WriteFile(k.records, raw, 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := newWriters(k.store, k.w.policy, k.limit, k.recordStore, k.records, time.Now); err == nil {
			t.Fatal("invalid record accepted", variant)
		}
	}
	// @1 remains closed even for a null new field.
	raw := `{"profile":"` + legacyRecordProfile + `","records":[{"native_owner":null}]}`
	if err := os.WriteFile(k.records, []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := newWriters(k.store, k.w.policy, k.limit, k.recordStore, k.records, time.Now); err == nil {
		t.Fatal("@1 accepted @2 field")
	}
}

func TestNativeWriterStaleLegacyLifetimeCannotReserveAfterCanonical(t *testing.T) {
	n := nativeWriterCases(t)
	k := newWriterCase(t, 1<<20)
	stale, err := newWriters(k.store, k.w.policy, k.limit, k.recordStore, k.records, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(stale.close)
	legacy := writeReceiver{stale, n[0].binding.legacy, nil, context.Background()}
	k.c = n[0].receiver(k.c)
	body := []byte("canonical reservation")
	request := strings.Repeat("s", 32)
	first, _ := k.c.Begin(request, named(body), int64(len(body)))
	if first.Outcome != api.BeginOutcomeStarted {
		t.Fatal(first)
	}
	places := k.store.places.Load()
	if result, _ := legacy.Begin(request, named(body), int64(len(body))); result.Outcome != api.BeginOutcomeUnavailable || k.store.places.Load() != places {
		t.Fatal("stale legacy lifetime staged duplicate", result)
	}
}

func TestNativeWriterVersionAliasesShareUploadLimit(t *testing.T) {
	n := nativeWriterCases(t)
	k := newWriterCase(t, 1<<20)
	for i := 0; i < MaxUploadsPerScope; i++ {
		body := []byte{byte(i)}
		receiver := n[i%2].receiver(k.c)
		result, _ := receiver.Begin(strings.Repeat(string(rune('a'+i)), 32), named(body), 1)
		if result.Outcome != api.BeginOutcomeStarted {
			t.Fatal(result)
		}
	}
	result, _ := n[1].receiver(k.c).Begin(strings.Repeat("z", 32), named([]byte("extra")), 5)
	if result.Outcome != api.BeginOutcomeExhausted {
		t.Fatal("alias bypassed per-subject bound", result)
	}
}

func TestNativeStorageReadHandlesAndOldContinuationsRetainOwnership(t *testing.T) {
	n := nativeWriterCases(t)
	k := newWriterCase(t, 1<<20)
	body := []byte("readable content")
	digest := named(body)
	first, _ := k.c.Begin(strings.Repeat("r", 32), digest, int64(len(body)))
	k.put(t, first.Upload.Handle, body, 0)
	k.c.Commit(first.Upload.Handle)
	registry := newRegistry(k.store, permit)
	t.Cleanup(registry.close)
	legacy := receiver{registry: registry, scope: n[0].binding.legacy, ctx: context.Background()}
	old, _ := legacy.Open(digest)
	if old.Resource == nil {
		t.Fatal(old)
	}
	currentWriter := n[0].receiver(k.c)
	current := receiver{registry: registry, scope: currentWriter.scope, peer: currentWriter.peer, ctx: currentWriter.ctx}
	if read, _ := current.Read(old.Resource.Handle, 0, 64); read.Outcome != api.ReadOutcomeData {
		t.Fatal("exact legacy handle lost", read)
	}
	versionWriter := n[1].receiver(k.c)
	version := receiver{registry: registry, scope: versionWriter.scope, peer: versionWriter.peer, ctx: versionWriter.ctx}
	if read, _ := version.Read(old.Resource.Handle, 0, 64); read.Outcome != api.ReadOutcomeForbidden {
		t.Fatal("old handle crossed versions", read)
	}
	fresh, _ := current.Open(digest)
	if read, _ := version.Read(fresh.Resource.Handle, 0, 64); read.Outcome != api.ReadOutcomeData {
		t.Fatal("canonical handle lost", read)
	}
	foreignWriter := n[2].receiver(k.c)
	foreign := receiver{registry: registry, scope: foreignWriter.scope, peer: foreignWriter.peer, ctx: foreignWriter.ctx}
	if read, _ := foreign.Read(fresh.Resource.Handle, 0, 64); read.Outcome != api.ReadOutcomeForbidden {
		t.Fatal("foreign canonical owner read", read)
	}
	journal, err := newChangeJournal(k.store, permit, permit, 8)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(journal.close)
	journal.snapshots["old"] = &snapshot{objects: []api.ListedObject{}, scope: n[0].binding.legacy, expires: time.Now().Add(SnapshotIdle)}
	changes := changesReceiver{journal: journal, scope: current.scope, peer: current.peer, ctx: current.ctx, wait: current.ctx}
	if page, _ := changes.List("old:0", 1); page.Outcome != api.ListingOutcomeForbidden {
		t.Fatal("old change continuation reinterpreted", page)
	}
	// Epoch cursors contain no ownership namespace and still filter per call.
	if page, _ := changes.Observe(journal.epoch+":0", 1, 0); page.Outcome != api.ChangePageOutcomePage {
		t.Fatal("compatible epoch cursor changed", page)
	}
	composed := composedReceiver()
	composed.scope = current.scope
	composed.peer = current.peer
	composed.ctx = current.ctx
	composed.inventory.snapshots["old"] = &inventorySnapshot{scope: n[0].binding.legacy, expires: time.Now().Add(SnapshotIdle)}
	if page, _ := composed.List("old:0", 1); page.Outcome != api.InventoryOutcomeForbidden {
		t.Fatal("old inventory continuation reinterpreted", page)
	}
}

func TestNativeBindingRefusesMissingCanonicalProgramInsteadOfLegacyFallback(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "app.exe")
	if err := os.WriteFile(path, []byte("inert"), 0600); err != nil {
		t.Fatal(err)
	}
	peer := identitytest.New(t, identitytest.Facts{User: identity.User{Kind: "windows", SID: "owner"}, Path: path + "\n"})[0]
	if callerScope(peer, "owner") == "" {
		t.Fatal("counterexample lacks exact legacy evidence")
	}
	if receivingNativeBinding(peer, "owner") != nil {
		t.Fatal("invalid canonical provenance admitted via legacy")
	}
}

type movedRecords struct {
	casapi.Store
	moved    bool
	observed error
}

func (m *movedRecords) Write(path string, base casapi.Value, data []byte) error {
	if m.moved {
		return m.Store.Write(path, base, data)
	}
	m.moved = true
	if err := os.WriteFile(path, []byte("competing writer"), 0600); err != nil {
		return err
	}
	m.observed = m.Store.Write(path, base, data)
	if err := os.WriteFile(path, base.Data, 0600); err != nil {
		return err
	}
	return m.observed
}

func TestNativeWriterCASConflictFencesAfterOriginalBytesReturn(t *testing.T) {
	n := nativeWriterCases(t)
	k := newWriterCase(t, 1<<20)
	raw := []byte(`{"profile":"` + legacyRecordProfile + `","records":[]}`)
	if err := os.WriteFile(k.records, raw, 0600); err != nil {
		t.Fatal(err)
	}
	moved := &movedRecords{Store: k.recordStore}
	w, err := newWriters(k.store, k.w.policy, k.limit, moved, k.records, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(w.close)
	caller := n[0].receiver(k.c)
	caller.writers = w
	body := []byte("one reservation")
	places, finds := k.store.places.Load(), k.store.finds.Load()
	first, _ := caller.Begin(strings.Repeat("m", 32), named(body), int64(len(body)))
	if first.Outcome != api.BeginOutcomeUnavailable || !errors.Is(moved.observed, cas.ErrMoved) {
		t.Fatal("control did not observe actual CAS conflict", first, moved.observed)
	}
	after, _ := os.ReadFile(k.records)
	if !bytes.Equal(raw, after) {
		t.Fatal("control did not restore original bytes")
	}
	second, _ := caller.Begin(strings.Repeat("n", 32), named(body), int64(len(body)))
	if second.Outcome != api.BeginOutcomeUnavailable || !w.fenced || k.store.places.Load() != places || k.store.finds.Load() != finds {
		t.Fatal("CAS fence was lost after bytes returned", second)
	}
}
