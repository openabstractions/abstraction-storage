package storage

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestDigestGrammarRefusesBeforeLookup pins W0-06(p): every non-canonical
// digest is refused before a store builds a path from it, including a
// traversal such as sha256:../../x and bare or uppercase hex.
func TestDigestGrammarRefusesBeforeLookup(t *testing.T) {
	hex := strings.Repeat("ab", 32)
	root := t.TempDir()
	cs, err := NewContentStore("c", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	fs := NewForeignStore("f", root, "")
	// Bytes exist under the bare hex name, so a lenient grammar would find them.
	if err := os.WriteFile(filepath.Join(root, hex), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, d := range []string{
		"sha256:../../x",
		"sha256:" + strings.Repeat("../", 21) + "x",
		hex,
		"sha256:" + strings.ToUpper(hex),
		"SHA256:" + hex,
		" sha256:" + hex,
		"sha256:" + hex + " ",
		"sha256-" + hex,
		"",
	} {
		if ValidDigest(d) {
			t.Errorf("ValidDigest(%q) = true", d)
		}
		if _, ok := cs.Find(d); ok {
			t.Errorf("content Find(%q) succeeded", d)
		}
		if _, ok := fs.Find(d); ok {
			t.Errorf("foreign Find(%q) succeeded", d)
		}
		if _, err := cs.Place(d, 1); err == nil {
			t.Errorf("Place(%q) accepted", d)
		}
		if err := cs.Remove(d); err == nil {
			t.Errorf("Remove(%q) accepted", d)
		}
	}
	if !ValidDigest("sha256:" + hex) {
		t.Fatal("canonical digest refused")
	}
	if _, ok := fs.Find("sha256:" + hex); !ok {
		t.Fatal("canonical digest not found")
	}
}
