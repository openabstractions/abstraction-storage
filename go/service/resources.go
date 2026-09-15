package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	identity "github.com/openabstractions/abstraction-identity"
	storage "github.com/openabstractions/abstraction-storage/go"
	api "github.com/openabstractions/abstraction-storage/go/abstraction/storage/content"
	"io"
	"os"
	"sync"
	"time"
)

// Policy explicitly authorizes this content. It must honor ctx and be safe for
// concurrent calls. Same-account transport identity does not authorize content.
type Policy func(context.Context, *identity.Peer, string) error

// ErrPolicyUnavailable distinguishes a failed decision lookup from a refusal.
// Policy callbacks may wrap it to retain their native diagnostic cause.
var ErrPolicyUnavailable = errors.New("storage: content policy unavailable")

const MaxResources = 32
const MaxPerScope = 8
const IdleLifetime = 30 * time.Second

type resource struct {
	mu            sync.Mutex
	file          *os.File
	initial       os.FileInfo
	digest, scope string
	expires       time.Time
}

func (r *resource) close() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.file != nil {
		r.file.Close()
		r.file = nil
	}
}

type registry struct {
	mu        sync.Mutex
	store     storage.Store
	policy    Policy
	resources map[string]*resource
	pending   map[string]int
	closed    bool
	now       func() time.Time
}

func newRegistry(store storage.Store, policy Policy) *registry {
	return &registry{store: store, policy: policy, resources: map[string]*resource{}, pending: map[string]int{}, now: time.Now}
}
func (r *registry) sweep() {
	r.mu.Lock()
	var expired []*resource
	now := r.now()
	for h, v := range r.resources {
		if !now.Before(v.expires) {
			delete(r.resources, h)
			expired = append(expired, v)
		}
	}
	r.mu.Unlock()
	for _, v := range expired {
		v.close()
	}
}
func (r *registry) close() {
	r.mu.Lock()
	r.closed = true
	old := r.resources
	r.resources = map[string]*resource{}
	r.mu.Unlock()
	for _, v := range old {
		v.close()
	}
}
func validDigest(d string) bool {
	if len(d) != 71 || d[:7] != "sha256:" {
		return false
	}
	for _, c := range d[7:] {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

type receiver struct {
	registry *registry
	scope    string
	peer     *identity.Peer
	ctx      context.Context
}

func (c receiver) authorization(digest string) string {
	return decide(c.ctx, c.scope, c.registry.policy, c.peer, digest)
}

// decide returns "" for an explicit permit, unavailable for a failed decision
// lookup and forbidden for every evaluated refusal.
func decide(ctx context.Context, scope string, policy Policy, peer *identity.Peer, digest string) string {
	if scope == "" {
		return "forbidden"
	}
	if ctx.Err() != nil {
		return "unavailable"
	}
	err := policy(ctx, peer, digest)
	if ctx.Err() != nil || errors.Is(err, ErrPolicyUnavailable) {
		return "unavailable"
	}
	if err != nil {
		return "forbidden"
	}
	return ""
}
func (c receiver) Open(digest string) (api.OpenResult, error) {
	result := func(s string) (api.OpenResult, error) { return api.OpenResult{Outcome: s}, nil }
	if !validDigest(digest) {
		return result("invalid")
	}
	if status := c.authorization(digest); status != "" {
		return result(status)
	}
	local, ok := c.registry.store.(storage.Local)
	if !ok {
		return result("unsupported")
	}
	r := c.registry
	r.sweep()
	r.mu.Lock()
	count, total := r.pending[c.scope], 0
	for _, n := range r.pending {
		total += n
	}
	total += len(r.resources)
	for _, v := range r.resources {
		if v.scope == c.scope {
			count++
		}
	}
	if r.closed {
		r.mu.Unlock()
		return result("unavailable")
	}
	if total >= MaxResources || count >= MaxPerScope {
		r.mu.Unlock()
		return result("exhausted")
	}
	r.pending[c.scope]++
	r.mu.Unlock()
	defer func() {
		r.mu.Lock()
		r.pending[c.scope]--
		if r.pending[c.scope] == 0 {
			delete(r.pending, c.scope)
		}
		r.mu.Unlock()
	}()
	ref, found := r.store.Find(digest)
	if !found {
		return result("not_found")
	}
	if ref.Digest != digest {
		return result("unsupported")
	}
	path := local.Path(ref)
	before, e := os.Lstat(path)
	if e != nil {
		return result("unavailable")
	}
	if !before.Mode().IsRegular() {
		return result("unsupported")
	}
	f, e := openRegular(path)
	if e != nil {
		return result("unavailable")
	}
	keep := false
	defer func() {
		if !keep {
			f.Close()
		}
	}()
	info, e := f.Stat()
	if e != nil || !info.Mode().IsRegular() || !os.SameFile(before, info) || info.Size() < 0 {
		return result("unavailable")
	}
	if c.ctx.Err() != nil {
		return result("unavailable")
	}
	var nonce [24]byte
	if _, e = rand.Read(nonce[:]); e != nil {
		return result("unavailable")
	}
	handle := hex.EncodeToString(nonce[:])
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return result("unavailable")
	}
	r.resources[handle] = &resource{file: f, initial: info, digest: digest, scope: c.scope, expires: r.now().Add(IdleLifetime)}
	keep = true
	return api.OpenResult{Outcome: "opened", Resource: &api.Resource{Handle: handle, Digest: digest, Size: info.Size(), Verification: "unverified"}}, nil
}
func (c receiver) find(handle string) (*resource, string) {
	r := c.registry
	r.sweep()
	r.mu.Lock()
	defer r.mu.Unlock()
	v := r.resources[handle]
	if v == nil {
		return nil, "gap"
	}
	if c.scope == "" || v.scope != c.scope {
		return nil, "forbidden"
	}
	return v, ""
}
func (c receiver) Read(handle string, offset, maxBytes int64) (api.ReadResult, error) {
	result := func(s string) (api.ReadResult, error) { return api.ReadResult{Outcome: s}, nil }
	if offset < 0 || maxBytes < 1 || maxBytes > 65536 || len(handle) > 128 {
		return result("invalid")
	}
	if c.scope == "" {
		return result("forbidden")
	}
	v, status := c.find(handle)
	if v == nil {
		return result(status)
	}
	if status := c.authorization(v.digest); status != "" {
		return result(status)
	}
	r := c.registry
	r.mu.Lock()
	if r.resources[handle] != v {
		r.mu.Unlock()
		return result("gap")
	}
	v.expires = r.now().Add(IdleLifetime)
	r.mu.Unlock()
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.file == nil {
		return result("gap")
	}
	changed := func() (api.ReadResult, error) {
		v.file.Close()
		v.file = nil
		r.mu.Lock()
		delete(r.resources, handle)
		r.mu.Unlock()
		return result("changed")
	}
	before, e := v.file.Stat()
	if e != nil {
		return result("unavailable")
	}
	if before.Size() != v.initial.Size() || !before.ModTime().Equal(v.initial.ModTime()) {
		return changed()
	}
	total := v.initial.Size()
	if offset > total {
		return result("invalid")
	}
	n := maxBytes
	if n > total-offset {
		n = total - offset
	}
	data := make([]byte, int(n))
	read, e := v.file.ReadAt(data, offset)
	if e != nil && e != io.EOF {
		return result("unavailable")
	}
	after, e := v.file.Stat()
	if e != nil {
		return result("unavailable")
	}
	if after.Size() != total || !after.ModTime().Equal(v.initial.ModTime()) || int64(read) != n {
		return changed()
	}
	if c.ctx.Err() != nil {
		return result("unavailable")
	}
	return api.ReadResult{Outcome: "data", Chunk: &api.Chunk{Offset: offset, Total: total, Data: data, Eof: offset+n == total}}, nil
}
func (c receiver) Close(handle string) (api.CloseResult, error) {
	if c.scope == "" {
		return api.CloseResult{Outcome: "forbidden"}, nil
	}
	v, status := c.find(handle)
	if v == nil {
		return api.CloseResult{Outcome: status}, nil
	}
	r := c.registry
	r.mu.Lock()
	if r.resources[handle] != v {
		r.mu.Unlock()
		return api.CloseResult{Outcome: "gap"}, nil
	}
	delete(r.resources, handle)
	r.mu.Unlock()
	v.close()
	return api.CloseResult{Outcome: "closed"}, nil
}
