package storage

import (
	"crypto/sha256"
	"fmt"
	"os"
	"testing"
)

func TestContentStoreRemoveIsRepeatable(t *testing.T) {
	store, err := NewContentStore("fixture", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	body := []byte("request input")
	digest := fmt.Sprintf("sha256:%x", sha256.Sum256(body))
	ref, err := store.Place(digest, int64(len(body)))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.Path(ref), body, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := store.Commit(ref); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := store.Remove(digest); err != nil {
			t.Fatal(err)
		}
		if _, ok := store.Find(digest); ok {
			t.Fatal("removed content is findable")
		}
	}
	if err := store.Remove("sha256:AB"); err == nil {
		t.Fatal("an invalid digest was accepted")
	}
}
