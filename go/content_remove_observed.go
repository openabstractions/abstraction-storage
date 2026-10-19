package storage

import (
	"context"
	"crypto/sha256"
	hexcodec "encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/openabstractions/abstraction-identity/fileid"
)

// RemovalProof is an observation of one committed file in this store's
// digest-derived namespace. An incoming reservation cannot supply a proof.
type RemovalProof struct {
	digest, file, directory string
	size, modified          int64
	persistent              bool
}

func (p RemovalProof) Size() int64         { return p.size }
func (p RemovalProof) Modified() time.Time { return time.Unix(0, p.modified).UTC() }
func (p RemovalProof) CanRetire() bool     { return p.persistent }

// Binding identifies the precise file observation without exposing native paths.
func (p RemovalProof) Binding() string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(fmt.Sprintf("%q/%q/%q/%d/%d", p.digest, p.file, p.directory, p.size, p.modified))))
}

var ErrRemovalNeedsAdministrator = errors.New("storage: access denied for native removal")
var ErrRemovalBusy = errors.New("storage: committed content is in use")
var ErrRemovalUnsupported = errors.New("storage: exclusive content deletion is unavailable on this platform")
var ErrRemovalChanged = errors.New("storage: committed content changed during removal")

// ObserveRemoval binds the file and its immediate parent by filesystem
// identity. It follows no link and reads no object bytes.
func (c *ContentStore) ObserveRemoval(ctx context.Context, digest string) (RemovalProof, error) {
	return c.observeRemoval(ctx, digest, "")
}

func (c *ContentStore) observeRemoval(ctx context.Context, digest, tombstone string) (RemovalProof, error) {
	hex := hexOf(digest)
	if hex == "" {
		return RemovalProof{}, ErrNotFound
	}
	dir, err := fileid.Open(ctx, fileid.Self, filepath.Dir(c.blob(hex)), fileid.Options{Links: fileid.RefuseLinks})
	if err != nil {
		return RemovalProof{}, err
	}
	defer dir.Close()
	name := filepath.Base(c.blob(hex))
	if tombstone != "" {
		name = tombstone
	}
	leaf, err := dir.Beneath(name, fileid.Options{})
	if err != nil {
		return RemovalProof{}, err
	}
	defer leaf.Close()
	proof, err := removalProof(digest, dir.File(), leaf.File())
	proof.persistent = dir.FileID().Durability() == fileid.Persistent && leaf.FileID().Durability() == fileid.Persistent
	return proof, err
}

func removalProof(digest string, dir, file *os.File) (RemovalProof, error) {
	info, err := file.Stat()
	if err != nil {
		return RemovalProof{}, err
	}
	if !info.Mode().IsRegular() {
		return RemovalProof{}, ErrRemovalChanged
	}
	fileKey, err := fileid.FileKeyOf(file)
	if err != nil {
		return RemovalProof{}, err
	}
	dirKey, err := fileid.FileKeyOf(dir)
	if err != nil {
		return RemovalProof{}, err
	}
	return RemovalProof{digest: digest, file: fileKey, directory: dirKey, size: info.Size(), modified: info.ModTime().UnixNano()}, nil
}

// RemoveObserved is the committed store owner's native deletion operation.
// The caller supplies the proof from its accepted storage observations. The
// provider reopens only its own digest-derived name and proves the exact file
// under its exclusive handle before marking it for deletion.
func (c *ContentStore) RemoveObserved(ctx context.Context, proof RemovalProof) error {
	return c.RemoveObservedAuthorized(ctx, proof, nil)
}

// RemoveObservedAuthorized calls authorize after proving the exact native
// file under exclusive custody and before marking it for deletion.
func (c *ContentStore) RemoveObservedAuthorized(ctx context.Context, proof RemovalProof, authorize func() error) error {
	return c.openObservedExclusive(ctx, proof, "", "", true, authorize)
}

// RetireObserved renames the exclusively held file out of the committed
// namespace after recording the exact answer. The renamed file survives a
// crash between the native effect and the Activity record.
func (c *ContentStore) RetireObserved(ctx context.Context, proof RemovalProof, occurrence string, authorize func() error) error {
	if !proof.CanRetire() {
		return ErrRemovalUnsupported
	}
	name, err := removalTombstone(proof.digest, occurrence)
	if err != nil {
		return err
	}
	return c.openObservedExclusive(ctx, proof, "", name, true, authorize)
}

// CompleteRetiredRemoval proves the exact renamed file under exclusive
// custody, settles its record, then drops the recovery file.
func (c *ContentStore) CompleteRetiredRemoval(ctx context.Context, digest, observation, occurrence string, settle func() error) error {
	name, err := removalTombstone(digest, occurrence)
	if err != nil {
		return err
	}
	proof, err := c.observeRemoval(ctx, digest, name)
	if err != nil {
		return err
	}
	if !proof.CanRetire() || proof.Binding() != observation || settle == nil {
		return ErrRemovalChanged
	}
	return c.openObservedExclusive(ctx, proof, name, "", true, settle)
}

// WalkRetiredRemovals visits occurrence names from the owner's private
// namespace. The callback must prove the corresponding file before settling.
func (c *ContentStore) WalkRetiredRemovals(ctx context.Context, visit func(digest, occurrence string)) error {
	dir, err := fileid.Open(ctx, fileid.Self, filepath.Dir(c.blob(strings.Repeat("0", 64))),
		fileid.Options{Links: fileid.RefuseLinks, Read: true})
	if err != nil {
		return err
	}
	defer dir.Close()
	for ctx.Err() == nil {
		names, err := dir.File().Readdirnames(128)
		for _, name := range names {
			part, ok := strings.CutPrefix(name, ".oa-removal-")
			key, occurrence, valid := strings.Cut(part, "-")
			digest := "sha256:" + key
			if canonical, err := removalTombstone(digest, occurrence); ok && valid && err == nil && name == canonical {
				visit(digest, occurrence)
			}
		}
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
	}
	return ctx.Err()
}

// VerifyArchivePublication checks committed content through a held, no-link
// path. Delayed settlement also reverifies every byte.
func (c *ContentStore) VerifyArchivePublication(ctx context.Context, digest string, size int64, key string, delayed bool) bool {
	hex := hexOf(digest)
	if hex == "" || key == "" {
		return false
	}
	path := c.blob(hex)
	dir, err := fileid.Open(ctx, fileid.Self, filepath.Dir(path), fileid.Options{Links: fileid.RefuseLinks})
	if err != nil {
		return false
	}
	defer dir.Close()
	leaf, err := dir.Beneath(filepath.Base(path), fileid.Options{Read: delayed})
	if err != nil {
		return false
	}
	defer leaf.Close()
	f := leaf.File()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() != size || ctx.Err() != nil {
		return false
	}
	if id := leaf.FileID(); id.Durability() != fileid.Persistent || id.Key() != key {
		return false
	}
	if !delayed {
		return true
	}
	sum := sha256.New()
	buffer := make([]byte, 256<<10)
	for ctx.Err() == nil {
		n, err := f.Read(buffer)
		_, _ = sum.Write(buffer[:n])
		if err != nil {
			if !errors.Is(err, io.EOF) || ctx.Err() != nil || hexcodec.EncodeToString(sum.Sum(nil)) != hex {
				return false
			}
			// Recheck the namespace and the read observation after a long
			// digest pass. The held handle alone could now name a moved file.
			current, err := fileid.Open(ctx, fileid.Self, filepath.Dir(path), fileid.Options{Links: fileid.RefuseLinks})
			if err != nil {
				return false
			}
			defer current.Close()
			if !current.FileID().Equal(dir.FileID()) {
				return false
			}
			named, err := current.Beneath(filepath.Base(path), fileid.Options{})
			if err != nil {
				return false
			}
			defer named.Close()
			after, err := named.File().Stat()
			return err == nil && named.FileID().Equal(leaf.FileID()) && after.Size() == size &&
				after.ModTime().Equal(info.ModTime()) && ctx.Err() == nil
		}
	}
	return false
}

func removalTombstone(digest, occurrence string) (string, error) {
	key := hexOf(digest)
	decoded, err := hexcodec.DecodeString(occurrence)
	if key == "" || err != nil || len(decoded) != 16 || hexcodec.EncodeToString(decoded) != occurrence {
		return "", ErrRemovalChanged
	}
	return ".oa-removal-" + key + "-" + occurrence, nil
}

// CheckObservedExclusive checks the native sharing and identity boundary
// without marking the file for removal.
func (c *ContentStore) CheckObservedExclusive(ctx context.Context, proof RemovalProof) error {
	return c.openObservedExclusive(ctx, proof, "", "", false, nil)
}

func (c *ContentStore) openObservedExclusive(ctx context.Context, proof RemovalProof, leaf, rename string, remove bool, authorize func() error) error {
	hex := hexOf(proof.digest)
	if hex == "" || proof.file == "" || proof.directory == "" {
		return ErrRemovalChanged
	}
	dir, err := fileid.Open(ctx, fileid.Self, filepath.Dir(c.blob(hex)), fileid.Options{Links: fileid.RefuseLinks})
	if err != nil {
		return fmt.Errorf("open committed namespace: %w", err)
	}
	defer dir.Close()
	dirKey, err := fileid.FileKeyOf(dir.File())
	if err != nil {
		return err
	}
	if dirKey != proof.directory {
		return ErrRemovalChanged
	}
	if leaf == "" {
		leaf = filepath.Base(c.blob(hex))
	} else {
		settle := authorize
		authorize = func() error {
			if err := flushRemovalDirectory(dir.File()); err != nil {
				return err
			}
			return settle()
		}
	}
	return removeExact(ctx, dir.File(), leaf, rename, proof, remove, authorize)
}
