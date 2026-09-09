package storage

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const digest = "sha256:" + "ab" + "cd" + "ef01" + "23456789" + "abcdef0123456789abcdef0123456789abcdef0123456789"

func hex(t *testing.T) string {
	t.Helper()
	h := hexOf(digest)
	if h == "" {
		t.Fatalf("test digest is malformed: %q", digest)
	}
	return h
}

// Bytes are not findable until they are committed. A half-written blob under
// its final name is a corrupt blob every other tool on the machine will trust,
// because it is named by a digest it does not have.
func TestPlacedBytesAreNotFindableUntilCommitted(t *testing.T) {
	c, err := NewContentStore("mine", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ref, err := c.Place(digest, 4)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := c.Find(digest); ok {
		t.Fatal("found before anything was written")
	}

	// Something writes there — us, a service, a daemon on another machine.
	if err := os.WriteFile(c.Path(ref), []byte("abcd"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, ok := c.Find(digest); ok {
		t.Fatal("found while still only placed — a reservation is not a delivery")
	}

	if err := c.Commit(ref); err != nil {
		t.Fatal(err)
	}
	got, ok := c.Find(digest)
	if !ok {
		t.Fatal("not found after commit")
	}
	if got.Size != 4 {
		t.Fatalf("size %d, want 4", got.Size)
	}
	if !strings.Contains(filepath.ToSlash(c.Path(got)), "blobs/sha256-"+hex(t)) {
		t.Fatalf("committed to %q, which is not the content-addressed name", c.Path(got))
	}
}

// Asking for something already held returns it rather than reserving a second
// place for it. Same rule the download service applies to jobs.
func TestPlacingWhatIsAlreadyHeldReturnsIt(t *testing.T) {
	c, _ := NewContentStore("mine", t.TempDir())
	ref, _ := c.Place(digest, 4)
	os.WriteFile(c.Path(ref), []byte("abcd"), 0o644)
	c.Commit(ref)

	held, _ := c.Find(digest)
	again, err := c.Place(digest, 4)
	if err != nil {
		t.Fatal(err)
	}
	// Not the ref that was placed before — that one named a reservation, and
	// the bytes have since moved to their content-addressed name. What matters
	// is that asking again hands back the bytes rather than a second place to
	// put them, because a second reservation means a second transfer of bytes
	// already on the disk.
	if c.Path(again) != c.Path(held) {
		t.Fatalf("placed %q for bytes already committed at %q", c.Path(again), c.Path(held))
	}
	if strings.Contains(filepath.ToSlash(c.Path(again)), "/incoming/") {
		t.Fatal("asking for bytes we already hold returned a reservation")
	}
}

// Committing twice is success. The process acknowledging a delivery may have
// restarted between the two attempts.
func TestCommitIsIdempotent(t *testing.T) {
	c, _ := NewContentStore("mine", t.TempDir())
	ref, _ := c.Place(digest, 4)
	os.WriteFile(c.Path(ref), []byte("abcd"), 0o644)
	if err := c.Commit(ref); err != nil {
		t.Fatal(err)
	}
	if err := c.Commit(ref); err != nil {
		t.Fatalf("second commit: %v", err)
	}
}

// Somebody else's cache is read. It is never written, because their layout is
// theirs to change and a tool that writes into another tool's store breaks when
// that tool upgrades.
func TestForeignStoresAreReadOnly(t *testing.T) {
	dir := t.TempDir()
	blob := filepath.Join(dir, "sha256-"+hexOf(digest))
	if err := os.WriteFile(blob, []byte("theirs"), 0o644); err != nil {
		t.Fatal(err)
	}
	f := NewForeignStore("ollama", dir, "sha256-")

	got, ok := f.Find(digest)
	if !ok {
		t.Fatal("did not find a blob named by its own digest")
	}
	if got.Store != "ollama" {
		t.Fatalf("store %q, want ollama", got.Store)
	}
	if _, err := f.Place(digest, 6); !errors.Is(err, ErrReadOnly) {
		t.Fatalf("Place returned %v, want ErrReadOnly", err)
	}
}

// The ordering is the feature: bytes we hold are preferred over bytes some
// other tool wrote, and Place skips read-only stores with no special case.
func TestStoresSearchInOrderAndPlaceWhereTheyCan(t *testing.T) {
	foreignDir := t.TempDir()
	os.WriteFile(filepath.Join(foreignDir, "sha256-"+hexOf(digest)), []byte("theirs"), 0o644)

	mine, _ := NewContentStore("mine", t.TempDir())
	all := New(NewForeignStore("ollama", foreignDir, "sha256-"), mine)

	// Only the foreign copy exists, so that is what is found.
	got, ok := all.Find(digest)
	if !ok || got.Store != "ollama" {
		t.Fatalf("found %+v, want the ollama copy", got)
	}

	// Place must skip the read-only store rather than fail.
	ref, err := all.Place(digest, 4)
	if err != nil {
		t.Fatalf("place: %v", err)
	}
	if ref.Store != "mine" {
		t.Fatalf("placed into %q, want the writable store", ref.Store)
	}
	os.WriteFile(all.Path(ref), []byte("ours"), 0o644)
	if err := all.Commit(ref); err != nil {
		t.Fatal(err)
	}

	// Now both hold it, and that is the 116 GB question answered.
	if n := len(all.FindAll(digest)); n != 2 {
		t.Fatalf("FindAll returned %d copies, want 2", n)
	}
}

// A store addressed by digest has nowhere to put bytes whose digest is unknown,
// and saying so beats inventing a name they would have to be moved off later.
func TestPlacingWithoutADigestIsRefused(t *testing.T) {
	c, _ := NewContentStore("mine", t.TempDir())
	if _, err := c.Place("", 0); err == nil {
		t.Fatal("placed bytes with no digest into a content-addressed store")
	}
	if _, err := c.Place("sha256:not-hex", 0); err == nil {
		t.Fatal("accepted a malformed digest")
	}
}

// An application must not be able to read a location out of the interface.
func TestRefDoesNotExposeALocation(t *testing.T) {
	c, _ := NewContentStore("mine", t.TempDir())
	ref, _ := c.Place(digest, 4)
	// The only way to a location is through the Local capability, which a
	// service-backed store would not implement.
	var s Store = c
	if _, ok := s.(Local); !ok {
		t.Fatal("a filesystem-backed store should advertise Local")
	}
	if Locator(ref) == "" {
		t.Fatal("the binding must still be able to resolve its own ref")
	}
}
