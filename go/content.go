package storage

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// ContentStore is a store of our own, addressed by digest.
//
// It is the write half, and it is what stops an application inventing a
// destination. Before it, download.Sink{Partial, Final} were two strings every
// caller filled in by hand — so the download layer was pluggable about who
// fetches the bytes and hardcoded about where they land.
//
// Layout:
//
//	<root>/blobs/sha256-<hex>          the bytes, once committed
//	<root>/incoming/sha256-<hex>       a reservation, until it is
//
// The same convention Ollama uses, deliberately: this is not a new format, it is
// the one two of the four measured stores already agreed on independently. A
// tool that reads Ollama's blobs directory can read this one.
//
// # Why placing and committing are separate
//
// Place settles WHERE and writes nothing. The process that eventually puts bytes
// there may be a Windows service under its own account, or a daemon on a NAS,
// running long after the process that asked has exited — so a reservation has to
// be a location rather than an open file.
//
// Commit is the moment the bytes become findable. Until then Find must not
// return them, because a half-written blob under its final name is a corrupt
// blob that every other tool on the machine will now trust: it is named by a
// digest it does not have.
type ContentStore struct {
	name string
	root string
}

func NewContentStore(name, root string) (*ContentStore, error) {
	for _, sub := range []string{"blobs", "incoming"} {
		if err := os.MkdirAll(filepath.Join(root, sub), 0o755); err != nil {
			return nil, err
		}
	}
	return &ContentStore{name: name, root: root}, nil
}

func (c *ContentStore) Name() string { return c.name }

func (c *ContentStore) blob(hex string) string {
	return filepath.Join(c.root, "blobs", "sha256-"+hex)
}

func (c *ContentStore) incoming(hex string) string {
	return filepath.Join(c.root, "incoming", "sha256-"+hex)
}

func (c *ContentStore) Find(digest string) (Ref, bool) {
	hex := hexOf(digest)
	if hex == "" {
		return Ref{}, false
	}
	st, err := os.Stat(c.blob(hex))
	if err != nil || st.IsDir() {
		return Ref{}, false
	}
	return NewRef(c.name, digest, c.blob(hex), st.Size()), true
}

// Place reserves a location. If the bytes are already here it returns them
// instead, so asking for something twice does not start a second transfer — the
// same rule the download service applies to jobs, one layer down.
func (c *ContentStore) Place(digest string, size int64) (Ref, error) {
	hex := hexOf(digest)
	if hex == "" {
		// A caller that does not know the digest yet is a real case: some
		// sources only reveal it on the way past. But this store is addressed
		// BY digest, so it has nowhere to put those bytes, and saying so is
		// better than inventing a name they would have to be moved off later.
		return Ref{}, fmt.Errorf("%w: a content-addressed store needs a digest to place by", ErrNotFound)
	}
	if r, ok := c.Find(digest); ok {
		return r, nil
	}
	return NewRef(c.name, digest, c.incoming(hex), size), nil
}

// Commit makes placed bytes findable, and refuses to do it for bytes that are
// not there.
func (c *ContentStore) Commit(r Ref) error {
	hex := hexOf(r.Digest)
	if hex == "" {
		return fmt.Errorf("%w: cannot commit without a digest", ErrNotFound)
	}
	from := c.incoming(hex)
	if _, err := os.Stat(from); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			// Already committed is success: a second acknowledgement of the
			// same delivery must not be an error, because the process doing the
			// acknowledging may have restarted.
			if _, ok := c.Find(r.Digest); ok {
				return nil
			}
			return fmt.Errorf("%w: nothing was placed for %s", ErrNotFound, r.Digest)
		}
		return err
	}
	// Rename, so a reader either sees no blob or a whole one. It must never see
	// a partial file under a name that claims a digest.
	return os.Rename(from, c.blob(hex))
}

func (c *ContentStore) Path(r Ref) string { return Locator(r) }

// Root is where this store keeps its bytes. For the binding and for diagnostics.
func (c *ContentStore) Root() string { return c.root }
