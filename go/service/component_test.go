package service

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	casapi "github.com/openabstractions/abstraction-cas/go/api"
	identity "github.com/openabstractions/abstraction-identity"
	storage "github.com/openabstractions/abstraction-storage/go"
	api "github.com/openabstractions/abstraction-storage/go/abstraction/storage/content"
)

func TestComponentRequiresPolicyAndKeepsUnauthenticatedCallOutOfStore(t *testing.T) {
	store := &countedStore{}
	if _, err := NewComponent(store, nil); err == nil {
		t.Fatal("missing read policy accepted")
	}
	// A dispatcher can be composed with other generated services. A call with
	// no native peer proof has no subject and cannot reach policy or Find.
	policyCalls := 0
	c, err := NewComponent(store, func(context.Context, *identity.Peer, string) error {
		policyCalls++
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	services := c.Services(nil, context.Background())
	if len(services) != 1 || services[0].ServiceContract() != "abstraction.storage/content-reader@1" {
		t.Fatalf("unexpected services: %v", services)
	}
	reader := services[0].(*api.ContentReaderDispatcher)
	opened, err := reader.Handler.Open(named([]byte("private")))
	if err != nil || opened.Outcome != api.OpenOutcomeForbidden || policyCalls != 0 || store.finds.Load() != 0 {
		t.Fatalf("unauthenticated open: %+v, %v, policy=%d, find=%d", opened, err, policyCalls, store.finds.Load())
	}
}

func TestComponentWriterConfigurationAndLifecycle(t *testing.T) {
	content, err := storage.NewContentStore("fixture", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	read := func(context.Context, *identity.Peer, string) error { return nil }
	c, err := NewComponent(content, read)
	if err != nil {
		t.Fatal(err)
	}
	records := casapi.BoundedFileStore{MaxBytes: MaxRecordFileBytes}
	path := filepath.Join(t.TempDir(), "identities.json")
	if err := c.EnableWriter(nil, 1, records, path); err == nil {
		t.Fatal("missing write policy accepted")
	}
	if err := c.EnableWriter(read, 1<<20, records, path); err != nil {
		t.Fatal(err)
	}
	if err := c.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := c.EnableWriter(read, 1<<20, records, path); err == nil {
		t.Fatal("writer reconfigured after start")
	}
	services := c.Services(nil, context.Background())
	if len(services) != 2 || services[1].ServiceContract() != "abstraction.storage/content-writer@1" {
		t.Fatalf("unexpected services: %v", services)
	}
	writer := services[1].(*api.ContentWriterDispatcher)
	begun, err := writer.Handler.Begin("test-request-1234", named([]byte("private")), 7)
	if err != nil || begun.Outcome != api.BeginOutcomeForbidden {
		t.Fatalf("unauthenticated begin: %+v, %v", begun, err)
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	if c.WriterAvailable() {
		t.Fatal("closed writer advertised")
	}
	if err := c.Start(context.Background()); err == nil {
		t.Fatal("closed component restarted")
	}
}

func TestComponentCloseCancelsComposedPolicyCall(t *testing.T) {
	store, err := storage.NewContentStore("fixture", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	entered := make(chan struct{})
	c, err := NewComponent(store, func(ctx context.Context, _ *identity.Peer, _ string) error {
		close(entered)
		<-ctx.Done()
		return ctx.Err()
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	reader := c.Services(nil, context.Background())[0].(*api.ContentReaderDispatcher).Handler.(receiver)
	// Supply a subject to exercise the policy call. Services itself derives a
	// real subject only from a framed peer; the no-peer case is tested above.
	reader.scope = "test-subject"
	done := make(chan api.OpenResult, 1)
	go func() { result, _ := reader.Open(named([]byte("private"))); done <- result }()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("policy did not start")
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case result := <-done:
		if result.Outcome != api.OpenOutcomeUnavailable {
			t.Fatalf("close outcome: %+v", result)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("close did not cancel policy")
	}
}
