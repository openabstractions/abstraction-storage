package layout

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"

	identity "github.com/openabstractions/abstraction-identity"
	"github.com/openabstractions/abstraction-identity/fileid"
)

// AccountStateRootNames is the existing closed reset roster. Empty native
// roots occur on platforms without account factory-reset. It creates nothing.
func AccountStateRootNames(local, roaming string) []string {
	var roots []string
	if local != "" {
		roots = append(roots, filepath.Join(local, Name))
	}
	if roaming != "" {
		for _, name := range []string{"abstraction", "OpenAbstractions", "Abstraction Panel"} {
			roots = append(roots, filepath.Join(roaming, name))
		}
	}
	return roots
}

// CheckLocalStatePath uses Identity's refusal of links in every component.
// Missing targets still require a safe nearest existing parent. No store is
// opened and no directory or lock is created.
func CheckLocalStatePath(ctx context.Context, path string) error {
	if err := fileid.LocalPath(path); err != nil {
		return err
	}
	if !filepath.IsAbs(path) || filepath.Dir(path) == path {
		return errors.New("factory-reset: unsafe root")
	}
	for p := path; ; p = filepath.Dir(p) {
		h, err := fileid.Open(ctx, fileid.Self, p, fileid.Options{Links: fileid.RefuseLinks})
		if err == nil {
			return h.Close()
		}
		if !errors.Is(err, fs.ErrNotExist) || filepath.Dir(p) == p {
			return err
		}
	}
}

// ErrAccountStateOverlap preserves the runtime private-state refusal.
var ErrAccountStateOverlap = errors.New("runtime: private state overlaps account settings")

// CheckPrivateStatePath preserves the runtime's native canonical containment
// test for a private path and the account roster, without opening account stores.
func CheckPrivateStatePath(ctx context.Context, path string, roots []string) error {
	if !filepath.IsAbs(path) {
		return fmt.Errorf("runtime: private state paths must be absolute")
	}
	if err := CheckLocalStatePath(ctx, path); err != nil {
		return err
	}
	name, err := fileid.Pending(ctx, fileid.Self, path)
	if err != nil {
		return err
	}
	for _, root := range roots {
		if !filepath.IsAbs(root) {
			return fmt.Errorf("runtime: unresolved account default metadata")
		}
		other, err := fileid.Pending(ctx, fileid.Self, root)
		if err != nil {
			return err
		}
		if identity.SamePathName(name.Path(), other.Path()) || identity.PathNameWithin(name.Path(), other.Path()) || identity.PathNameWithin(other.Path(), name.Path()) {
			return ErrAccountStateOverlap
		}
	}
	return nil
}
