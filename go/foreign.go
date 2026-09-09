package storage

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ForeignStore is somebody else's content-addressed store, read and never
// written.
//
// This is the half that pays for the design. Once a registry has told us an
// artifact's digest, finding out whether those exact bytes are already on this
// disk is a stat rather than a download — and it does not require hashing 116 GB
// either, because these stores already name their files by content:
//
//	Ollama            blobs/sha256-<hex>     named by digest
//	HuggingFace       blobs/<etag>           the etag IS the sha256, for LFS
//	Lemonade          reads the HuggingFace layout
//
// Read-only, deliberately. Their layouts are theirs to change, and a tool that
// writes into another tool's cache is a tool that breaks when the other one
// upgrades. Place returns ErrReadOnly.
//
// LM Studio is absent on purpose, and it is the argument in miniature: it was
// the largest store on the machine that was measured, 44 GB, and it publishes no
// digest — so nothing can tell whether its bytes are the same as anyone else's.
// That is not a limitation of this code.
type ForeignStore struct {
	name string
	// dir holds content-named files.
	dir string
	// prefix is what the store puts before the hex digest, if anything. Ollama
	// writes "sha256-"; HuggingFace writes nothing.
	prefix string
}

func NewForeignStore(name, dir, prefix string) *ForeignStore {
	return &ForeignStore{name: name, dir: dir, prefix: prefix}
}

func (f *ForeignStore) Name() string { return f.name }

func (f *ForeignStore) Find(digest string) (Ref, bool) {
	hex := hexOf(digest)
	if hex == "" {
		return Ref{}, false
	}
	p := filepath.Join(f.dir, f.prefix+hex)
	st, err := os.Stat(p)
	if err != nil || st.IsDir() {
		return Ref{}, false
	}
	return NewRef(f.name, digest, p, st.Size()), true
}

func (f *ForeignStore) Place(string, int64) (Ref, error) {
	return Ref{}, fmt.Errorf("%w: %s", ErrReadOnly, f.name)
}

func (f *ForeignStore) Path(r Ref) string { return Locator(r) }

// Discover finds the content-addressed stores belonging to other tools on this
// user's machine.
//
// Presence is the configuration here too: a machine without Ollama simply has
// no Ollama store, and nothing has to be told about it.
func Discover() []Store {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	var out []Store
	out = append(out, NewForeignStore("ollama", filepath.Join(home, ".ollama", "models", "blobs"), "sha256-"))

	// The HuggingFace cache keeps one blobs/ directory per repo, so every repo
	// is its own root. Enumerating them is cheap — one readdir of names, not a
	// walk of the contents.
	for _, hub := range []struct{ name, dir string }{
		{"huggingface", filepath.Join(home, ".cache", "huggingface", "hub")},
		// Lemonade reads the HuggingFace layout, but also writes plain files
		// with no blobs directory, so only the cache half is addressable.
		{"lemonade", filepath.Join(home, ".cache", "lemonade", "hub")},
	} {
		entries, err := os.ReadDir(hub.dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() && strings.HasPrefix(e.Name(), "models--") {
				out = append(out, NewForeignStore(hub.name, filepath.Join(hub.dir, e.Name(), "blobs"), ""))
			}
		}
	}
	return out
}

func hexOf(digest string) string {
	hex := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(digest)), "sha256:")
	if len(hex) != 64 {
		return ""
	}
	for _, c := range hex {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return ""
		}
	}
	return hex
}
