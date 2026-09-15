package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	casapi "github.com/openabstractions/abstraction-cas/go/api"
	identity "github.com/openabstractions/abstraction-identity"
	storage "github.com/openabstractions/abstraction-storage/go"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type countedStore struct {
	*storage.ContentStore
	finds, places atomic.Int32
	commits       atomic.Int32
	beforeCommit  func() error
}

func (s *countedStore) Commit(r storage.Ref) error {
	s.commits.Add(1)
	if s.beforeCommit != nil {
		if err := s.beforeCommit(); err != nil {
			return err
		}
	}
	return s.ContentStore.Commit(r)
}

// flakyRecords fails record writes while fail is set, as a full or failing
// state volume would.
type flakyRecords struct {
	casapi.Store
	fail atomic.Bool
}

func (s *flakyRecords) Write(path string, base casapi.Value, data []byte) error {
	if s.fail.Load() {
		return errors.New("record volume unavailable")
	}
	return s.Store.Write(path, base, data)
}

func (s *countedStore) Find(d string) (storage.Ref, bool) {
	s.finds.Add(1)
	return s.ContentStore.Find(d)
}
func (s *countedStore) Place(d string, n int64) (storage.Ref, error) {
	s.places.Add(1)
	return s.ContentStore.Place(d, n)
}

type writerCase struct {
	w           *writers
	c           writeReceiver
	store       *countedStore
	allowed     *atomic.Bool
	online      *atomic.Bool
	root        string
	limit       int64
	records     string
	recordStore *flakyRecords
}

func newWriterCase(t *testing.T, limit int64) writerCase {
	t.Helper()
	root := t.TempDir()
	content, err := storage.NewContentStore("fixture", root)
	if err != nil {
		t.Fatal(err)
	}
	store := &countedStore{ContentStore: content}
	var allowed, online atomic.Bool
	allowed.Store(true)
	online.Store(true)
	k := writerCase{store: store, allowed: &allowed, online: &online, root: root, limit: limit, records: filepath.Join(t.TempDir(), "writer-records.json"), recordStore: &flakyRecords{Store: casapi.BoundedFileStore{MaxBytes: MaxRecordFileBytes}}}
	k.restart(t, time.Now)
	return k
}

// restart constructs a new writer lifetime over the same provider and records.
// The previous lifetime is abandoned without close, as after a crash.
func (k *writerCase) restart(t *testing.T, now func() time.Time) {
	t.Helper()
	if k.w != nil {
		// Process exit closes file handles and leaves staged bytes in place.
		k.w.mu.Lock()
		for _, u := range k.w.uploads {
			u.mu.Lock()
			if u.file != nil {
				u.file.Close()
				u.file = nil
			}
			u.mu.Unlock()
		}
		k.w.mu.Unlock()
	}
	w, err := newWriters(k.store, func(ctx context.Context, _ *identity.Peer, _ string) error {
		if !k.online.Load() {
			return ErrPolicyUnavailable
		}
		if !k.allowed.Load() {
			return errors.New("write denied")
		}
		return ctx.Err()
	}, k.limit, k.recordStore, k.records, now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(w.close)
	k.w, k.c = w, writeReceiver{w, "account-program", nil, context.Background()}
}

func named(body []byte) string { return fmt.Sprintf("sha256:%x", sha256.Sum256(body)) }

func (k writerCase) staging(digest string) string {
	return filepath.Join(k.root, "incoming", "sha256-"+digest[7:])
}

func (k writerCase) blobs(t *testing.T) int {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(k.root, "blobs"))
	if err != nil {
		t.Fatal(err)
	}
	return len(entries)
}

func (k writerCase) put(t *testing.T, handle string, body []byte, from int) {
	t.Helper()
	for offset := from; offset < len(body); {
		end := min(offset+MaxAppendBytes, len(body))
		r, _ := k.c.Append(handle, int64(offset), body[offset:end])
		if r.Outcome != "accepted" || r.Received != int64(end) {
			t.Fatalf("append %d: %+v", offset, r)
		}
		offset = end
	}
}

func TestWriterCommitIsAtomicAndIdempotent(t *testing.T) {
	k := newWriterCase(t, 1<<20)
	body := bytes.Repeat([]byte("atomic-bytes"), 12000)
	digest := named(body)
	request := strings.Repeat("r", 32)
	begun, _ := k.c.Begin(request, digest, int64(len(body)))
	if begun.Outcome != "started" || begun.Upload == nil || begun.Limit != 1<<20 {
		t.Fatal(begun)
	}
	handle := begun.Upload.Handle
	first, _ := k.c.Append(handle, 0, body[:MaxAppendBytes])
	if first.Outcome != "accepted" {
		t.Fatal(first)
	}
	if _, ok := k.store.ContentStore.Find(digest); ok {
		t.Fatal("staged bytes are findable")
	}
	retried, _ := k.c.Begin(request, digest, int64(len(body)))
	if retried.Outcome != "started" || retried.Upload.Handle != handle || retried.Upload.Received != MaxAppendBytes {
		t.Fatalf("lost Begin reply retry %+v", retried)
	}
	skipped, _ := k.c.Append(handle, 0, body[:10])
	if skipped.Outcome != "out_of_order" || skipped.Received != MaxAppendBytes {
		t.Fatal(skipped)
	}
	early, _ := k.c.Commit(handle)
	if early.Outcome != "incomplete" || early.Received != MaxAppendBytes {
		t.Fatal(early)
	}
	k.put(t, handle, body, MaxAppendBytes)
	over, _ := k.c.Append(handle, int64(len(body)), []byte("x"))
	if over.Outcome != "too_large" || over.Received != int64(len(body)) {
		t.Fatal(over)
	}
	committed, _ := k.c.Commit(handle)
	if committed.Outcome != "committed" || committed.Stored == nil || committed.Stored.Evidence != "hashed" || committed.Stored.Size != int64(len(body)) {
		t.Fatal(committed)
	}
	ref, ok := k.store.ContentStore.Find(digest)
	if !ok {
		t.Fatal("committed content not findable")
	}
	if got, err := os.ReadFile(k.store.Path(ref)); err != nil || !bytes.Equal(got, body) {
		t.Fatal("committed bytes differ", err)
	}
	again, _ := k.c.Commit(handle)
	if again.Outcome != "gap" {
		t.Fatal(again)
	}
	duplicate, _ := k.c.Begin(request, digest, int64(len(body)))
	if duplicate.Outcome != "committed" || duplicate.Stored.Evidence != "hashed" {
		t.Fatal(duplicate)
	}
	conflict, _ := k.c.Begin(request, named([]byte("other")), 5)
	if conflict.Outcome != "conflict" {
		t.Fatal(conflict)
	}
	resized, _ := k.c.Begin(request, digest, 1)
	if resized.Outcome != "conflict" {
		t.Fatal(resized)
	}
	present, _ := k.c.Begin(strings.Repeat("p", 32), digest, int64(len(body)))
	if present.Outcome != "present" || present.Stored.Evidence != "named" {
		t.Fatal(present)
	}
	if k.blobs(t) != 1 {
		t.Fatal("duplicate objects")
	}
	if _, err := os.Stat(k.staging(digest)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("staging remains after commit", err)
	}
}

func TestWriterRefusalsHaveNoEffects(t *testing.T) {
	k := newWriterCase(t, 100)
	body := []byte("authorized bytes")
	digest := named(body)
	request := strings.Repeat("a", 16)
	for _, bad := range []struct {
		request, digest string
		size            int64
	}{{"short", digest, 1}, {strings.Repeat("a", 129), digest, 1}, {strings.Repeat("a", 15) + "/", digest, 1}, {request, "sha256:AB", 1}, {request, digest, -1}} {
		if r, _ := k.c.Begin(bad.request, bad.digest, bad.size); r.Outcome != "invalid" {
			t.Fatal(bad, r)
		}
	}
	k.allowed.Store(false)
	if r, _ := k.c.Begin(request, digest, int64(len(body))); r.Outcome != "forbidden" || r.Limit != 0 {
		t.Fatal(r)
	}
	k.allowed.Store(true)
	k.online.Store(false)
	if r, _ := k.c.Begin(request, digest, int64(len(body))); r.Outcome != "unavailable" {
		t.Fatal(r)
	}
	k.online.Store(true)
	anonymous := k.c
	anonymous.scope = ""
	if r, _ := anonymous.Begin(request, digest, int64(len(body))); r.Outcome != "forbidden" {
		t.Fatal(r)
	}
	if r, _ := k.c.Begin(request, digest, 101); r.Outcome != "too_large" || r.Limit != 100 {
		t.Fatal(r)
	}
	if k.store.finds.Load() != 0 || k.store.places.Load() != 0 {
		t.Fatal("refused Begin reached provider")
	}
	begun, _ := k.c.Begin(request, digest, int64(len(body)))
	if begun.Outcome != "started" {
		t.Fatal(begun)
	}
	handle := begun.Upload.Handle
	if r, _ := k.c.Append(handle, 0, body[:5]); r.Outcome != "accepted" {
		t.Fatal(r)
	}
	foreign := k.c
	foreign.scope = "another-program"
	if a, _ := foreign.Append(handle, 5, body[5:]); a.Outcome != "forbidden" {
		t.Fatal(a)
	}
	if c, _ := foreign.Commit(handle); c.Outcome != "forbidden" {
		t.Fatal(c)
	}
	if a, _ := foreign.Abort(handle); a.Outcome != "forbidden" {
		t.Fatal(a)
	}
	if b, _ := foreign.Begin(request, digest, int64(len(body))); b.Outcome != "busy" {
		t.Fatal("same digest by another scope", b)
	}
	k.allowed.Store(false)
	if a, _ := k.c.Append(handle, 5, body[5:]); a.Outcome != "forbidden" {
		t.Fatal(a)
	}
	if info, err := os.Stat(k.staging(digest)); err != nil || info.Size() != 5 {
		t.Fatal("revoked append staged bytes", err)
	}
	k.allowed.Store(true)
	k.put(t, handle, body, 5)
	k.allowed.Store(false)
	if c, _ := k.c.Commit(handle); c.Outcome != "forbidden" {
		t.Fatal(c)
	}
	if _, ok := k.store.ContentStore.Find(digest); ok {
		t.Fatal("revoked commit became visible")
	}
	if a, _ := k.c.Abort(handle); a.Outcome != "aborted" {
		t.Fatal("abort after revocation", a)
	}
	if _, err := os.Stat(k.staging(digest)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("aborted staging remains", err)
	}
	if a, _ := k.c.Append(handle, 0, body); a.Outcome != "gap" {
		t.Fatal(a)
	}
	k.allowed.Store(true)
	if again, _ := k.c.Begin(request, digest, int64(len(body))); again.Outcome != "started" || again.Upload.Received != 0 {
		t.Fatal("aborted identity is reusable", again)
	}
}

func TestWriterIdentitySurvivesRestartWithinRetention(t *testing.T) {
	k := newWriterCase(t, 1<<20)
	body := []byte("committed before restart")
	digest := named(body)
	committed := strings.Repeat("c", 24)
	begun, _ := k.c.Begin(committed, digest, int64(len(body)))
	k.put(t, begun.Upload.Handle, body, 0)
	if r, _ := k.c.Commit(begun.Upload.Handle); r.Outcome != "committed" {
		t.Fatal(r)
	}
	partial := []byte("interrupted before restart")
	partialDigest := named(partial)
	unfinished := strings.Repeat("u", 24)
	started, _ := k.c.Begin(unfinished, partialDigest, int64(len(partial)))
	if r, _ := k.c.Append(started.Upload.Handle, 0, partial[:6]); r.Outcome != "accepted" {
		t.Fatal(r)
	}
	if _, err := os.Stat(k.staging(partialDigest)); err != nil {
		t.Fatal("staging missing before crash", err)
	}
	other := named([]byte("different content"))

	// Crash: the old lifetime is never closed and its staging remains.
	restarted := time.Now()
	k.restart(t, func() time.Time { return restarted })
	if _, err := os.Stat(k.staging(partialDigest)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("restart left staging of an unfinished upload", err)
	}
	places := k.store.places.Load()
	if r, _ := k.c.Begin(committed, other, 17); r.Outcome != "conflict" {
		t.Fatal("changed content under a committed identity after restart", r)
	}
	if r, _ := k.c.Begin(unfinished, other, 17); r.Outcome != "conflict" {
		t.Fatal("changed content under an unfinished identity after restart", r)
	}
	if k.store.places.Load() != places {
		t.Fatal("conflict reached provider placement")
	}
	again, _ := k.c.Begin(committed, digest, int64(len(body)))
	if again.Outcome != "committed" || again.Stored.Evidence != "hashed" || again.Stored.Size != int64(len(body)) {
		t.Fatal("committed identity lost its result", again)
	}
	resumed, _ := k.c.Begin(unfinished, partialDigest, int64(len(partial)))
	if resumed.Outcome != "started" || resumed.Upload.Received != 0 {
		t.Fatal("unfinished identity did not restart its upload", resumed)
	}
	k.put(t, resumed.Upload.Handle, partial, 0)
	if r, _ := k.c.Commit(resumed.Upload.Handle); r.Outcome != "committed" {
		t.Fatal(r)
	}

	// Past retention the identity is forgotten and may name new content.
	k.restart(t, func() time.Time { return restarted.Add(RequestRetention + time.Second) })
	if r, _ := k.c.Begin(committed, other, 17); r.Outcome != "started" {
		t.Fatal("expired identity still refused", r)
	}

	// Another writer of the record file fences this lifetime.
	if err := os.WriteFile(k.records, []byte(`{"profile":"`+RecordProfile+`","records":[]}`), 0600); err != nil {
		t.Fatal(err)
	}
	places = k.store.places.Load()
	if r, _ := k.c.Begin(strings.Repeat("f", 24), named([]byte("fenced")), 6); r.Outcome != "unavailable" || k.store.places.Load() != places {
		t.Fatal("record file changed by another owner was overwritten", r)
	}

	// A malformed record file refuses the lifetime instead of forgetting identities.
	if err := os.WriteFile(k.records, []byte(`{"profile":"`+RecordProfile+`","records":[{"scope":"s"}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := newWriters(k.store, k.w.policy, k.limit, casapi.BoundedFileStore{MaxBytes: MaxRecordFileBytes}, k.records, time.Now); err == nil {
		t.Fatal("malformed writer records accepted")
	}
}

func TestWriterRecordsCommitIntentBeforePublishing(t *testing.T) {
	k := newWriterCase(t, 1<<20)
	upload := func(request string, body []byte) string {
		t.Helper()
		begun, _ := k.c.Begin(request, named(body), int64(len(body)))
		if begun.Outcome != "started" {
			t.Fatal(begun)
		}
		k.put(t, begun.Upload.Handle, body, 0)
		return begun.Upload.Handle
	}

	// Record saves fail from the moment the provider publishes. The result was
	// recorded before publishing, so a restarted retry still reads committed.
	published := []byte("published while the record volume failed")
	handle := upload(strings.Repeat("p", 20), published)
	k.store.beforeCommit = func() error { k.recordStore.fail.Store(true); return nil }
	if r, _ := k.c.Commit(handle); r.Outcome != "committed" {
		t.Fatal(r)
	}
	k.store.beforeCommit = nil
	k.recordStore.fail.Store(false)
	k.restart(t, time.Now)
	if r, _ := k.c.Begin(strings.Repeat("p", 20), named(published), int64(len(published))); r.Outcome != "committed" || r.Stored.Evidence != "hashed" {
		t.Fatal("published result degraded after restart", r)
	}

	// A failed intent save publishes nothing.
	unsaved := []byte("intent could not be recorded")
	handle = upload(strings.Repeat("s", 20), unsaved)
	commits := k.store.commits.Load()
	k.recordStore.fail.Store(true)
	if r, _ := k.c.Commit(handle); r.Outcome != "unavailable" || k.store.commits.Load() != commits {
		t.Fatal("commit published without a recorded intent", r)
	}
	k.recordStore.fail.Store(false)
	if _, ok := k.store.ContentStore.Find(named(unsaved)); ok {
		t.Fatal("unrecorded commit became visible")
	}

	// A crash after the intent save and before publishing leaves a recorded
	// result with no content. Startup demotes it to an unfinished identity.
	crashed := []byte("intent recorded, publish never happened")
	handle = upload(strings.Repeat("x", 20), crashed)
	k.store.beforeCommit = func() error { k.recordStore.fail.Store(true); return errors.New("process ended") }
	if r, _ := k.c.Commit(handle); r.Outcome != "unavailable" {
		t.Fatal(r)
	}
	k.store.beforeCommit = nil
	k.recordStore.fail.Store(false)
	k.restart(t, time.Now)
	if r, _ := k.c.Begin(strings.Repeat("x", 20), named(crashed), int64(len(crashed))); r.Outcome != "started" || r.Upload.Received != 0 {
		t.Fatal("unpublished recorded result was reported", r)
	}
	if r, _ := k.c.Begin(strings.Repeat("x", 20), named(published), int64(len(published))); r.Outcome != "conflict" {
		t.Fatal("demoted identity accepted different content", r)
	}
}

func TestWriterMismatchExpiryLimitsAndClose(t *testing.T) {
	k := newWriterCase(t, 1<<20)
	claimed := named([]byte("expected"))
	request := strings.Repeat("m", 20)
	begun, _ := k.c.Begin(request, claimed, 8)
	k.put(t, begun.Upload.Handle, []byte("tampered"), 0)
	if c, _ := k.c.Commit(begun.Upload.Handle); c.Outcome != "mismatch" {
		t.Fatal(c)
	}
	if _, ok := k.store.ContentStore.Find(claimed); ok {
		t.Fatal("mismatch became visible")
	}
	if _, err := os.Stat(k.staging(claimed)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("mismatch staging remains", err)
	}
	if again, _ := k.c.Begin(request, claimed, 8); again.Outcome != "started" {
		t.Fatal(again)
	}
	now := time.Now()
	k.w.mu.Lock()
	k.w.now = func() time.Time { return now }
	k.w.mu.Unlock()
	body := []byte("interrupted upload")
	digest := named(body)
	idle, _ := k.c.Begin(strings.Repeat("i", 20), digest, int64(len(body)))
	k.c.Append(idle.Upload.Handle, 0, body[:4])
	k.w.mu.Lock()
	k.w.now = func() time.Time { return now.Add(IdleLifetime + time.Second) }
	k.w.mu.Unlock()
	if a, _ := k.c.Append(idle.Upload.Handle, 4, body[4:]); a.Outcome != "gap" {
		t.Fatal("expired upload", a)
	}
	if _, err := os.Stat(k.staging(digest)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("expired staging remains", err)
	}
	// Expiry also discarded the earlier restarted upload; this scope is empty.
	for i := 0; i < MaxUploadsPerScope; i++ {
		if r, _ := k.c.Begin(fmt.Sprintf("scope-limit-%08d", i), named([]byte{byte(i)}), 1); r.Outcome != "started" {
			t.Fatal(i, r)
		}
	}
	if r, _ := k.c.Begin("scope-limit-overflow", named([]byte("overflow")), 1); r.Outcome != "exhausted" {
		t.Fatal(r)
	}
	k.w.close()
	if entries, err := os.ReadDir(filepath.Join(k.root, "incoming")); err != nil || len(entries) != 0 {
		t.Fatal("shutdown left staged uploads", len(entries), err)
	}
	if r, _ := k.c.Begin(strings.Repeat("z", 20), digest, 1); r.Outcome != "unavailable" {
		t.Fatal(r)
	}
	if k.blobs(t) != 0 {
		t.Fatal("unexpected committed objects")
	}
}
