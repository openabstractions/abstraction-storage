package service

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	storage "github.com/openabstractions/abstraction-storage/go"
	api "github.com/openabstractions/abstraction-storage/go/abstraction/storage/content"
)

// slowSource answers no Snapshot within the caller's budget: it waits for
// the call's own deadline and then fails, as a transport timeout does.
type slowSource struct{ budget time.Duration }

func (s slowSource) Snapshot(string, int64) (api.SourcePage, error) {
	time.Sleep(s.budget)
	return api.SourcePage{}, errors.New("deadline exceeded")
}

func (slowSource) Verify(string) (api.SourcePage, error) {
	return api.SourcePage{}, errors.New("deadline exceeded")
}

// One source slower than its budget leaves the others' models listed: its
// accepted stores are marked unread and the composition names it incomplete.
// Only a composition no source answers is refused.
func TestComposeListsTheSourcesThatAnswered(t *testing.T) {
	fast := &fakeSource{pages: []api.SourcePage{sourcePage(
		[]api.Store{{Name: "lmstudio", Present: true, Placement: api.PlacementLocal, Errors: []api.StoreError{}}},
		[]api.ManifestHolders{namedManifest("lmstudio", "m-tiny", "test/Tiny-GGUF/Tiny-135M-Q4_K_M.gguf")}, nil, nil, "")}}
	slow := slowSource{budget: 200 * time.Millisecond}
	designated := []DesignatedSource{
		{Program: "slow-inventoryd", Stores: []string{"huggingface"}, Reader: slow},
		{Program: "inventoryd", Stores: []string{"lmstudio"}, Reader: fast},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	start := time.Now()
	composed, err := Compose(ctx, designated)
	if err != nil {
		t.Fatalf("one slow source refused the composition: %v", err)
	}
	if took := time.Since(start); took > 2*time.Second {
		t.Fatalf("the composition took %v; the sources are read at once", took)
	}
	if len(composed.Manifests) != 1 || composed.Manifests[0].Manifest.ID != "m-tiny" {
		t.Fatalf("manifests %+v, want the fast source's m-tiny", composed.Manifests)
	}
	if !slices.Equal(composed.Incomplete, []string{"slow-inventoryd"}) {
		t.Fatalf("incomplete %v, want the slow source", composed.Incomplete)
	}
	var unread, read *api.Store
	for i := range composed.Stores {
		switch composed.Stores[i].Name {
		case "huggingface":
			unread = &composed.Stores[i]
		case "lmstudio":
			read = &composed.Stores[i]
		}
	}
	if unread == nil || len(unread.Errors) != 1 || unread.Errors[0].Detail != storage.UnansweredSourceError {
		t.Fatalf("the slow source's store %+v, want it marked unread", unread)
	}
	if read == nil || len(read.Errors) != 0 {
		t.Fatalf("the fast source's store %+v, want it read", read)
	}

	if _, err := Compose(ctx, designated[:1]); !errors.Is(err, errInventoryUnavailable) {
		t.Fatalf("no source answered and the composition read %v, want unavailable", err)
	}
}
