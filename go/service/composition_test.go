package service

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
)

func TestComposedReadUsesReaderChecks(t *testing.T) {
	r, _, digest, path := fixture(t)
	allow := func(context.Context, string) error { return nil }
	ctx := context.Background()
	data, outcome := ReadAuthorized(ctx, r.store, allow, digest, 100)
	if outcome != "read" || string(data) != "unverified fixture bytes" {
		t.Fatalf("%q %q", data, outcome)
	}
	data, outcome = ReadAuthorized(ctx, r.store, allow, digest, 2)
	if outcome != "too_large" || data != nil {
		t.Fatalf("oversize: %q %q", data, outcome)
	}
	_, outcome = ReadAuthorized(ctx, r.store, allow, "sha256:"+strings.Repeat("b", 64), 100)
	if outcome != "not_found" {
		t.Fatal(outcome)
	}
	store := &counted{Store: r.store}
	_, outcome = ReadAuthorized(ctx, store, func(context.Context, string) error { return errors.New("denied") }, digest, 100)
	if outcome != "forbidden" || store.finds.Load() != 0 {
		t.Fatalf("denied: %s, lookups %d", outcome, store.finds.Load())
	}
	if err := os.WriteFile(path, make([]byte, 65537), 0600); err != nil {
		t.Fatal(err)
	}
	calls := 0
	data, outcome = ReadAuthorized(ctx, r.store, func(context.Context, string) error {
		calls++
		if calls == 3 {
			return errors.New("revoked")
		}
		return nil
	}, digest, 100000)
	if outcome != "forbidden" || data != nil || calls != 3 {
		t.Fatalf("revoked: %s, bytes %d, calls %d", outcome, len(data), calls)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	data, outcome = ReadAuthorized(cancelled, r.store, allow, digest, 100000)
	if outcome != "unavailable" || data != nil {
		t.Fatalf("cancelled: %s, bytes %d", outcome, len(data))
	}
}
