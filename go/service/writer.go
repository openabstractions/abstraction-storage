package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	casapi "github.com/openabstractions/abstraction-cas/go/api"
	identity "github.com/openabstractions/abstraction-identity"
	storage "github.com/openabstractions/abstraction-storage/go"
	api "github.com/openabstractions/abstraction-storage/go/abstraction/storage/content"
	"hash"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const MaxUploads = 16
const MaxUploadsPerScope = 4
const MaxRequestRecords = 256
const MaxAppendBytes = 65536

// RequestRetention bounds how long a request identity is remembered after its
// last Begin, Commit or present result, across provider restarts.
const RequestRetention = 10 * time.Minute

// WritableStore is the provider shape the writer adapts. Place chooses the
// staging location without effects, Local projects it for this service, and
// Commit makes the complete staged bytes findable in one provider step.
type WritableStore interface {
	storage.Store
	storage.Local
	storage.Writable
}

type upload struct {
	mu             sync.Mutex
	file           *os.File
	path           string
	ref            storage.Ref
	sum            hash.Hash
	handle, key    string
	digest, scope  string
	size, received int64
	ready, done    bool
	expires        time.Time
}

// discard releases the staged bytes and returns what failed. The caller has
// already removed the upload from every index.
func (u *upload) discard() error {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.done = true
	if u.file == nil {
		return nil
	}
	err := errors.Join(closeStaged(u.path, u.file.Close()), removeStaged(u.path))
	u.file = nil
	return err
}

// closeStaged names a failed close of a staging file.
func closeStaged(path string, err error) error {
	if err != nil {
		return fmt.Errorf("storage writer: close staging %s: %w", path, err)
	}
	return nil
}

// removeStaged deletes a staging file. One that is already gone is not a failure.
func removeStaged(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("storage writer: remove staging %s: %w", path, err)
	}
	return nil
}

// savedIdentities names a failed save of the request identity record.
func savedIdentities(err error) error {
	if err != nil {
		return fmt.Errorf("storage writer: persist request identities: %w", err)
	}
	return nil
}

// requestRecord is one identity. handle names its live upload; an empty handle
// with no stored result is an unfinished identity that Begin may resume anew.
type requestRecord struct {
	digest  string
	size    int64
	handle  string
	stored  *api.Stored
	expires time.Time
}

type writers struct {
	mu          sync.Mutex
	store       WritableStore
	policy      Policy
	limit       int64
	uploads     map[string]*upload
	digests     map[string]*upload
	records     map[string]*requestRecord
	recordStore casapi.Store
	recordPath  string
	recordBase  casapi.Value
	closed      bool
	now         func() time.Time
	// onCommitted is called after a successful publish, outside the writer
	// mutex. It is set before Serve and never changed afterwards.
	onCommitted func(digest string, size int64)
	// report receives failures no reply carries: staging cleanup, and identity
	// saves whose outcome the reply has already fixed. It is set before Serve;
	// nil drops them.
	report func(error)
	// unreported holds staging cleanup failures from load until report is set.
	unreported error
}

// failed hands a non-nil failure to report. Call it without holding w.mu.
func (w *writers) failed(err error) {
	if err != nil && w.report != nil {
		w.report(err)
	}
}

// newWriters restores durable identities before any call is served.
func newWriters(store WritableStore, policy Policy, limit int64, records casapi.Store, recordPath string, now func() time.Time) (*writers, error) {
	w := &writers{store: store, policy: policy, limit: limit, uploads: map[string]*upload{}, digests: map[string]*upload{}, records: map[string]*requestRecord{}, recordStore: records, recordPath: recordPath, now: now}
	if err := w.load(); err != nil {
		return nil, err
	}
	return w, nil
}

// unlink removes an upload from the indexes. forget deletes its identity;
// otherwise the identity remains unfinished. It reports whether persisted state
// changed. The caller holds w.mu.
func (w *writers) unlink(u *upload, forget bool) bool {
	if w.uploads[u.handle] == u {
		delete(w.uploads, u.handle)
	}
	if w.digests[u.digest] == u {
		delete(w.digests, u.digest)
	}
	rec := w.records[u.key]
	if rec == nil || rec.handle != u.handle {
		return false
	}
	if forget {
		delete(w.records, u.key)
		return true
	}
	rec.handle = ""
	return false
}

func (w *writers) sweep() {
	w.mu.Lock()
	now := w.now()
	var expired []*upload
	for _, u := range w.uploads {
		if u.ready && !now.Before(u.expires) {
			w.unlink(u, false)
			expired = append(expired, u)
		}
	}
	changed := false
	for key, rec := range w.records {
		if (rec.stored != nil || rec.handle == "") && !now.Before(rec.expires) {
			delete(w.records, key)
			changed = true
		}
	}
	var saveErr error
	if changed && !w.closed {
		saveErr = savedIdentities(w.saveLocked())
	}
	w.mu.Unlock()
	w.failed(saveErr)
	for _, u := range expired {
		w.failed(u.discard())
	}
}

// close discards staged uploads. Persisted identities remain for restart.
func (w *writers) close() {
	w.mu.Lock()
	w.closed = true
	old := w.uploads
	w.uploads, w.digests, w.records = map[string]*upload{}, map[string]*upload{}, map[string]*requestRecord{}
	w.mu.Unlock()
	for _, u := range old {
		w.failed(u.discard())
	}
}

func validRequest(request string) bool {
	if len(request) < 16 || len(request) > 128 {
		return false
	}
	for _, c := range request {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-') {
			return false
		}
	}
	return true
}

type writeReceiver struct {
	writers *writers
	scope   string
	peer    *identity.Peer
	ctx     context.Context
}

func (c writeReceiver) authorization(digest string) string {
	return decide(c.ctx, c.scope, c.writers.policy, c.peer, digest)
}

func (c writeReceiver) Begin(request, digest string, size int64) (api.BeginResult, error) {
	w := c.writers
	result := func(s string) (api.BeginResult, error) { return api.BeginResult{Outcome: s, Limit: w.limit}, nil }
	unavailable := api.BeginResult{Outcome: "unavailable"}
	if !validRequest(request) || !validDigest(digest) || size < 0 {
		return api.BeginResult{Outcome: "invalid"}, nil
	}
	if status := c.authorization(digest); status != "" {
		return api.BeginResult{Outcome: status}, nil
	}
	if size > w.limit {
		return result("too_large")
	}
	w.sweep()
	key := c.scope + "\x00" + request
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return unavailable, nil
	}
	rec := w.records[key]
	if rec != nil {
		if rec.digest != digest || rec.size != size {
			w.mu.Unlock()
			return result("conflict")
		}
		if rec.stored != nil && rec.handle == "" {
			stored := *rec.stored
			w.mu.Unlock()
			outcome := "committed"
			if stored.Evidence == "named" {
				outcome = "present"
			}
			return api.BeginResult{Outcome: outcome, Stored: &stored, Limit: w.limit}, nil
		}
		if rec.handle != "" {
			u := w.uploads[rec.handle]
			if u == nil || !u.ready {
				w.mu.Unlock()
				return result("busy")
			}
			u.expires = w.now().Add(IdleLifetime)
			w.mu.Unlock()
			u.mu.Lock()
			defer u.mu.Unlock()
			if u.done {
				// A commit in progress finished while this call waited.
				w.mu.Lock()
				var stored *api.Stored
				if w.records[key] == rec && rec.handle == "" && rec.stored != nil {
					copied := *rec.stored
					stored = &copied
				}
				w.mu.Unlock()
				if stored != nil {
					return api.BeginResult{Outcome: "committed", Stored: stored, Limit: w.limit}, nil
				}
				return result("busy")
			}
			return api.BeginResult{Outcome: "started", Upload: &api.Upload{Handle: u.handle, Digest: u.digest, Size: u.size, Received: u.received}, Limit: w.limit}, nil
		}
		// An unfinished identity from an expired upload or an earlier provider
		// lifetime starts a new upload of the same content.
	}
	if w.digests[digest] != nil {
		w.mu.Unlock()
		return result("busy")
	}
	perScope := 0
	for _, u := range w.uploads {
		if u.scope == c.scope {
			perScope++
		}
	}
	if len(w.uploads) >= MaxUploads || perScope >= MaxUploadsPerScope || rec == nil && len(w.records) >= MaxRequestRecords {
		w.mu.Unlock()
		return result("exhausted")
	}
	var nonce [24]byte
	if _, e := rand.Read(nonce[:]); e != nil {
		w.mu.Unlock()
		return unavailable, nil
	}
	u := &upload{handle: hex.EncodeToString(nonce[:]), key: key, digest: digest, scope: c.scope, size: size}
	w.uploads[u.handle], w.digests[digest] = u, u
	created := rec == nil
	if created {
		rec = &requestRecord{digest: digest, size: size}
		w.records[key] = rec
	}
	previousExpiry := rec.expires
	rec.handle, rec.expires = u.handle, w.now().Add(RequestRetention)
	// The identity is durable before any provider lookup or staging effect.
	if err := w.saveLocked(); err != nil {
		delete(w.uploads, u.handle)
		delete(w.digests, digest)
		if created {
			delete(w.records, key)
		} else {
			rec.handle, rec.expires = "", previousExpiry
		}
		w.mu.Unlock()
		return unavailable, nil
	}
	w.mu.Unlock()

	outcome, file, path, ref := c.prepare(u)
	w.mu.Lock()
	if outcome == "present" {
		w.unlink(u, false)
		stored := api.Stored{Digest: digest, Size: ref.Size, Evidence: "named"}
		var saveErr error
		if w.records[key] == rec {
			rec.stored, rec.expires = &stored, w.now().Add(RequestRetention)
			saveErr = savedIdentities(w.saveLocked())
		}
		w.mu.Unlock()
		w.failed(saveErr)
		return api.BeginResult{Outcome: "present", Stored: &stored, Limit: w.limit}, nil
	}
	if outcome == "" && (w.closed || w.uploads[u.handle] != u || c.ctx.Err() != nil) {
		outcome = "unavailable"
	}
	if outcome != "" {
		var saveErr error
		if w.unlink(u, true) {
			saveErr = savedIdentities(w.saveLocked())
		}
		w.mu.Unlock()
		w.failed(saveErr)
		if file != nil {
			w.failed(errors.Join(closeStaged(path, file.Close()), removeStaged(path)))
		}
		if outcome == "unavailable" {
			return unavailable, nil
		}
		return result(outcome)
	}
	u.mu.Lock()
	u.file, u.path, u.ref, u.sum, u.ready = file, path, ref, sha256.New(), true
	u.expires = w.now().Add(IdleLifetime)
	u.mu.Unlock()
	w.mu.Unlock()
	return api.BeginResult{Outcome: "started", Upload: &api.Upload{Handle: u.handle, Digest: digest, Size: size}, Limit: w.limit}, nil
}

// prepare runs trusted provider callbacks outside the writer mutex. It creates
// only a staging file at the provider's reservation, which Find never reports.
func (c writeReceiver) prepare(u *upload) (string, *os.File, string, storage.Ref) {
	store := c.writers.store
	if found, ok := store.Find(u.digest); ok {
		return "present", nil, "", found
	}
	ref, e := store.Place(u.digest, u.size)
	if errors.Is(e, storage.ErrReadOnly) {
		return "unsupported", nil, "", storage.Ref{}
	}
	if e != nil || ref.Digest != u.digest {
		return "unavailable", nil, "", storage.Ref{}
	}
	path := store.Path(ref)
	if path == "" || !filepath.IsAbs(path) {
		return "unsupported", nil, "", storage.Ref{}
	}
	if before, e := os.Lstat(path); e == nil && !before.Mode().IsRegular() {
		return "unsupported", nil, "", storage.Ref{}
	} else if e != nil && !errors.Is(e, os.ErrNotExist) {
		return "unavailable", nil, "", storage.Ref{}
	}
	f, e := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0600)
	if e != nil {
		return "unavailable", nil, "", storage.Ref{}
	}
	info, e := f.Stat()
	after, le := os.Lstat(path)
	if e != nil || le != nil || !info.Mode().IsRegular() || !os.SameFile(info, after) {
		c.writers.failed(closeStaged(path, f.Close()))
		return "unavailable", nil, "", storage.Ref{}
	}
	return "", f, path, ref
}

func (c writeReceiver) find(handle string) (*upload, string) {
	w := c.writers
	w.sweep()
	w.mu.Lock()
	defer w.mu.Unlock()
	u := w.uploads[handle]
	if u == nil || !u.ready {
		return nil, "gap"
	}
	if c.scope == "" || u.scope != c.scope {
		return nil, "forbidden"
	}
	return u, ""
}

func (c writeReceiver) Append(handle string, offset int64, data []byte) (api.AppendResult, error) {
	result := func(s string, received int64) (api.AppendResult, error) {
		return api.AppendResult{Outcome: s, Received: received}, nil
	}
	if offset < 0 || len(data) < 1 || len(data) > MaxAppendBytes || len(handle) > 128 {
		return result("invalid", 0)
	}
	if c.scope == "" {
		return result("forbidden", 0)
	}
	u, status := c.find(handle)
	if u == nil {
		return result(status, 0)
	}
	if status := c.authorization(u.digest); status != "" {
		return result(status, 0)
	}
	w := c.writers
	w.mu.Lock()
	if w.uploads[handle] != u {
		w.mu.Unlock()
		return result("gap", 0)
	}
	u.expires = w.now().Add(IdleLifetime)
	w.mu.Unlock()
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.done || u.file == nil {
		return result("gap", 0)
	}
	if offset != u.received {
		return result("out_of_order", u.received)
	}
	if int64(len(data)) > u.size-u.received {
		return result("too_large", u.received)
	}
	if c.ctx.Err() != nil {
		return result("unavailable", 0)
	}
	n, e := u.file.WriteAt(data, offset)
	if e == nil && n == len(data) {
		// A hash.Hash write does not fail. Checking it keeps the digest and the
		// staged length in step with the same cleanup as a failed write.
		_, e = u.sum.Write(data)
	}
	if e != nil || n != len(data) {
		// A failed write leaves staged length uncertain. Discard the bytes; the
		// identity stays unfinished and a later Begin starts again.
		cleanup := errors.Join(closeStaged(u.path, u.file.Close()), removeStaged(u.path))
		u.file = nil
		u.done = true
		w.mu.Lock()
		w.unlink(u, false)
		w.mu.Unlock()
		w.failed(cleanup)
		return result("unavailable", 0)
	}
	u.received += int64(n)
	return result("accepted", u.received)
}

func (c writeReceiver) Commit(handle string) (api.CommitResult, error) {
	result := func(s string) (api.CommitResult, error) { return api.CommitResult{Outcome: s}, nil }
	if len(handle) > 128 {
		return result("gap")
	}
	if c.scope == "" {
		return result("forbidden")
	}
	u, status := c.find(handle)
	if u == nil {
		return result(status)
	}
	if status := c.authorization(u.digest); status != "" {
		return result(status)
	}
	w := c.writers
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.done || u.file == nil {
		return result("gap")
	}
	if u.received != u.size {
		return api.CommitResult{Outcome: "incomplete", Received: u.received}, nil
	}
	if c.ctx.Err() != nil {
		return result("unavailable")
	}
	drop := func(outcome string, forget bool) (api.CommitResult, error) {
		var closeErr error
		if u.file != nil {
			closeErr = closeStaged(u.path, u.file.Close())
			u.file = nil
		}
		cleanup := errors.Join(closeErr, removeStaged(u.path))
		u.done = true
		w.mu.Lock()
		var saveErr error
		if w.unlink(u, forget) && !w.closed {
			saveErr = savedIdentities(w.saveLocked())
		}
		w.mu.Unlock()
		w.failed(errors.Join(cleanup, saveErr))
		return result(outcome)
	}
	if "sha256:"+hex.EncodeToString(u.sum.Sum(nil)) != u.digest {
		return drop("mismatch", true)
	}
	if e := u.file.Sync(); e != nil {
		return drop("unavailable", false)
	}
	if e := u.file.Close(); e != nil {
		u.file = nil
		return drop("unavailable", false)
	}
	u.file = nil
	stored := api.Stored{Digest: u.digest, Size: u.size, Evidence: "hashed"}
	// Record the committed result before publishing. A retry after a completed
	// publish then reads committed even if no later save succeeds. A recorded
	// result whose content never became findable is demoted here or at startup.
	w.mu.Lock()
	rec := w.records[u.key]
	if w.closed || rec == nil || rec.handle != u.handle {
		w.mu.Unlock()
		return drop("unavailable", false)
	}
	previous := rec.expires
	rec.stored, rec.expires = &stored, w.now().Add(RequestRetention)
	if err := w.saveLocked(); err != nil {
		rec.stored, rec.expires = nil, previous
		w.mu.Unlock()
		return drop("unavailable", false)
	}
	w.mu.Unlock()
	published := w.store.Commit(u.ref) == nil
	if published {
		found, ok := w.store.Find(u.digest)
		published = ok && found.Digest == u.digest
	}
	if !published {
		w.mu.Lock()
		var saveErr error
		if w.records[u.key] == rec && rec.stored == &stored {
			rec.stored, rec.expires = nil, previous
			if !w.closed {
				saveErr = savedIdentities(w.saveLocked())
			}
		}
		w.mu.Unlock()
		w.failed(saveErr)
		return drop("unavailable", false)
	}
	u.done = true
	w.mu.Lock()
	w.unlink(u, false)
	committed := w.onCommitted
	w.mu.Unlock()
	if committed != nil {
		committed(u.digest, u.size)
	}
	return api.CommitResult{Outcome: "committed", Stored: &stored}, nil
}

func (c writeReceiver) Abort(handle string) (api.AbortResult, error) {
	if c.scope == "" {
		return api.AbortResult{Outcome: "forbidden"}, nil
	}
	u, status := c.find(handle)
	if u == nil {
		return api.AbortResult{Outcome: status}, nil
	}
	w := c.writers
	w.mu.Lock()
	if w.uploads[handle] != u {
		w.mu.Unlock()
		return api.AbortResult{Outcome: "gap"}, nil
	}
	var saveErr error
	if w.unlink(u, true) && !w.closed {
		saveErr = savedIdentities(w.saveLocked())
	}
	w.mu.Unlock()
	w.failed(errors.Join(saveErr, u.discard()))
	return api.AbortResult{Outcome: "aborted"}, nil
}
