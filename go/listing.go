package storage

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// Lister is an OPTIONAL capability: a store that can enumerate the objects it
// has committed, so an observer can learn about bytes that arrived or left
// without passing through this process.
//
// It reports committed objects only. A reservation that has not been committed
// is not an object yet, for the same reason Find does not report it.
type Lister interface {
	// List returns every committed object with its digest and observed size. It
	// must not hash anything and must return at most max objects; a store
	// holding more returns ErrTooMany rather than a partial list that would look
	// like deletions.
	List(max int) ([]Ref, error)
}

// ErrTooMany reports a listing larger than the caller's bound.
var ErrTooMany = errors.New("storage: listing exceeds its bound")

// List enumerates committed blobs named sha256-<hex>. Entries that are not
// regular files or not canonical names are ignored.
func (c *ContentStore) List(max int) ([]Ref, error) {
	entries, err := os.ReadDir(filepath.Join(c.root, "blobs"))
	if err != nil {
		return nil, err
	}
	var out []Ref
	for _, e := range entries {
		hex, ok := strings.CutPrefix(e.Name(), "sha256-")
		if !ok || hexOf("sha256:"+hex) != hex || !e.Type().IsRegular() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return nil, err
		}
		if len(out) == max {
			return nil, ErrTooMany
		}
		out = append(out, NewRef(c.name, "sha256:"+hex, c.blob(hex), info.Size()))
	}
	return out, nil
}
