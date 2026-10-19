package storage

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestRetiredRemovalWalkRefusesLinkedBlobDirectory(t *testing.T) {
	store, err := NewContentStore("runtime-content", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	blobs := filepath.Join(store.Root(), "blobs")
	other := t.TempDir()
	digest := "sha256:" + strings.Repeat("a", 64)
	name, err := removalTombstone(digest, strings.Repeat("b", 32))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(other, name), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(blobs, blobs+".old"); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("cmd", "/c", "mklink", "/J", blobs, other).CombinedOutput(); err != nil {
		t.Fatalf("fixture junction: %v %s", err, out)
	}
	t.Cleanup(func() { _ = os.Remove(blobs) })
	err = store.WalkRetiredRemovals(t.Context(), func(_, _ string) { t.Error("walk entered substituted blob directory") })
	if err == nil {
		t.Fatal("linked blob directory was accepted")
	}
}

func TestContentStoreRemoveObservedKeepsAnOpenOrReplacedFile(t *testing.T) {
	const digest = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	store, err := NewContentStore("runtime-content", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ref, err := store.Place(digest, 8)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.Path(ref), []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ObserveRemoval(t.Context(), digest); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("observe staged input: %v", err)
	}
	if err := store.Commit(ref); err != nil {
		t.Fatal(err)
	}
	current, found := store.Find(digest)
	if !found {
		t.Fatal("committed content not found")
	}
	path := store.Path(current)
	proof, err := store.ObserveRemoval(t.Context(), digest)
	if err != nil {
		t.Fatal(err)
	}
	reader, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reader.Close() })
	if err := store.RemoveObserved(t.Context(), proof); !errors.Is(err, ErrRemovalBusy) {
		t.Fatalf("exclusive removal with reader: %v", err)
	}
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	// Keep the original allocated, so the replacement necessarily has a
	// different filesystem identity even if it has the original bytes.
	if err := os.Rename(path, path+"-original"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := store.RemoveObserved(t.Context(), proof); !errors.Is(err, ErrRemovalChanged) {
		t.Fatalf("exclusive removal with substituted file: %v", err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(path+"-original", path); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(filepath.Dir(path), "same-file-link")
	if err := os.Link(path, link); err != nil {
		t.Fatal(err)
	}
	if err := store.RemoveObserved(t.Context(), proof); !errors.Is(err, ErrRemovalChanged) {
		t.Fatalf("exclusive removal with a second link: %v", err)
	}
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	if err := store.RemoveObserved(cancelled, proof); !errors.Is(err, context.Canceled) {
		t.Fatalf("exclusive removal after cancellation: %v", err)
	}
	if err := store.RemoveObserved(t.Context(), proof); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("committed name still exists: %v", err)
	}
	if _, err := store.ObserveRemoval(t.Context(), "sha256:"+strings.Repeat(".", 64)); !errors.Is(err, ErrNotFound) {
		t.Fatalf("accepted an arbitrary locator: %v", err)
	}
}
