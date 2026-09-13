package service

import (
	"context"
	"errors"
	identity "github.com/openabstractions/abstraction-identity"
	storage "github.com/openabstractions/abstraction-storage/go"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func fixture(t *testing.T) (*registry, receiver, string, string) {
	t.Helper()
	dir := t.TempDir()
	digest := "sha256:" + strings.Repeat("a", 64)
	path := filepath.Join(dir, strings.Repeat("a", 64))
	if e := os.WriteFile(path, []byte("unverified fixture bytes"), 0600); e != nil {
		t.Fatal(e)
	}
	r := newRegistry(storage.NewForeignStore("fixture", dir, ""), func(context.Context, *identity.Peer, string) error { return nil })
	t.Cleanup(r.close)
	return r, receiver{r, "account-program", nil, context.Background()}, digest, path
}
func TestResourceScopeExpiryRevocationAndMutation(t *testing.T) {
	r, c, d, path := fixture(t)
	opened, e := c.Open(d)
	if e != nil || opened.Outcome != "opened" || opened.Resource.Verification != "unverified" {
		t.Fatal(opened, e)
	}
	h := opened.Resource.Handle
	foreign := c
	foreign.scope = "another-program"
	x, _ := foreign.Read(h, 0, 3)
	if x.Outcome != "forbidden" {
		t.Fatal(x)
	}
	first, _ := c.Read(h, 0, 3)
	if string(first.Chunk.Data) != "unv" || first.Chunk.Eof {
		t.Fatal(first)
	}
	if e := os.WriteFile(path, []byte("changed and longer fixture bytes"), 0600); e != nil {
		t.Fatal(e)
	}
	changed, _ := c.Read(h, 3, 3)
	if changed.Outcome != "changed" || changed.Chunk != nil {
		t.Fatal(changed)
	}
	gap, _ := c.Read(h, 0, 3)
	if gap.Outcome != "gap" {
		t.Fatal(gap)
	}
	opened, _ = c.Open(d)
	r.policy = func(context.Context, *identity.Peer, string) error { return errors.New("revoked") }
	denied, _ := c.Read(opened.Resource.Handle, 0, 3)
	if denied.Outcome != "forbidden" {
		t.Fatal(denied)
	}
	closed, _ := c.Close(opened.Resource.Handle)
	if closed.Outcome != "closed" {
		t.Fatal(closed)
	}
	r.policy = func(context.Context, *identity.Peer, string) error { return nil }
	opened, _ = c.Open(d)
	r.now = func() time.Time { return time.Now().Add(IdleLifetime + time.Second) }
	expired, _ := c.Read(opened.Resource.Handle, 0, 3)
	if expired.Outcome != "gap" {
		t.Fatal(expired)
	}
}

type counted struct {
	storage.Store
	finds atomic.Int32
}

func (c *counted) Find(d string) (storage.Ref, bool) { c.finds.Add(1); return c.Store.Find(d) }
func TestPolicyBeforeLookupAndMissingLocal(t *testing.T) {
	r, c, d, _ := fixture(t)
	store := &counted{Store: r.store}
	r.store = store
	r.policy = func(context.Context, *identity.Peer, string) error { return errors.New("denied") }
	x, _ := c.Open(d)
	if x.Outcome != "forbidden" || store.finds.Load() != 0 {
		t.Fatal(x, store.finds.Load())
	}
	r.policy = func(context.Context, *identity.Peer, string) error { return nil }
	x, _ = c.Open(d)
	if x.Outcome != "unsupported" || store.finds.Load() != 0 {
		t.Fatal(x)
	}
	if h, e := Listen("unused", store, nil); e == nil || h != nil {
		t.Fatal("missing policy accepted")
	}
}
func TestResourceLimitsShutdownAndNoCallbackGlobalLock(t *testing.T) {
	r, c, d, _ := fixture(t)
	for i := 0; i < MaxPerScope; i++ {
		x, _ := c.Open(d)
		if x.Outcome != "opened" {
			t.Fatal(x)
		}
	}
	x, _ := c.Open(d)
	if x.Outcome != "exhausted" {
		t.Fatal(x)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	r.policy = func(context.Context, *identity.Peer, string) error { close(entered); <-release; return nil }
	done := make(chan struct{})
	go func() { c.Open(d); close(done) }()
	<-entered
	closed := make(chan struct{})
	go func() { r.close(); close(closed) }()
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("policy held global lock")
	}
	close(release)
	<-done
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.resources) != 0 || len(r.pending) != 0 {
		t.Fatal("resources survived close")
	}
}
func TestReadBoundsAndExactEOF(t *testing.T) {
	_, c, d, _ := fixture(t)
	opened, _ := c.Open(d)
	resource := opened.Resource
	x, _ := c.Read(resource.Handle, resource.Size, 1)
	if x.Outcome != "data" || !x.Chunk.Eof || len(x.Chunk.Data) != 0 {
		t.Fatal(x)
	}
	for _, n := range []int64{-1, 0, 65537} {
		x, _ = c.Read(resource.Handle, 0, n)
		if x.Outcome != "invalid" || x.Chunk != nil {
			t.Fatal(x)
		}
	}
}
