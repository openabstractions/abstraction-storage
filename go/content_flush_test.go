package storage

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func placeFixture(t *testing.T) (*ContentStore, Ref, string) {
	t.Helper()
	store, err := NewContentStore("fixture", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	body := []byte("flushed bytes")
	digest := fmt.Sprintf("sha256:%x", sha256.Sum256(body))
	ref, err := store.Place(digest, int64(len(body)))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.Path(ref), body, 0o600); err != nil {
		t.Fatal(err)
	}
	return store, ref, digest
}

func TestContentCommitFlushesDirectory(t *testing.T) {
	store, ref, digest := placeFixture(t)
	flushed := ""
	old := syncDir
	syncDir = func(dir string) error { flushed = dir; return old(dir) }
	t.Cleanup(func() { syncDir = old })
	if err := store.Commit(ref); err != nil {
		t.Fatal(err)
	}
	r, ok := store.Find(digest)
	if !ok || flushed == "" {
		t.Fatalf("commit found=%v flushed=%q", ok, flushed)
	}
	if want := Locator(r); flushed != filepath.Dir(want) {
		t.Fatalf("flushed %q, blob in %q", flushed, filepath.Dir(want))
	}
}

func TestContentCommitFsyncFailureDiscards(t *testing.T) {
	for _, point := range []string{"file", "dir"} {
		t.Run(point, func(t *testing.T) {
			store, ref, digest := placeFixture(t)
			injected := errors.New("injected fsync failure")
			oldPath, oldDir := syncPath, syncDir
			t.Cleanup(func() { syncPath, syncDir = oldPath, oldDir })
			if point == "file" {
				syncPath = func(string) error { return injected }
			} else {
				syncDir = func(string) error { return injected }
			}
			err := store.Commit(ref)
			if !errors.Is(err, ErrUnavailable) {
				t.Fatalf("commit answered %v, want unavailable", err)
			}
			if _, ok := store.Find(digest); ok {
				t.Fatal("a blob of unknown durability is findable")
			}
			if _, err := os.Stat(store.Path(ref)); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("the reservation was kept: %v", err)
			}
		})
	}
}
