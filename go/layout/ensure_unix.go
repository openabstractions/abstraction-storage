//go:build unix

package layout

import (
	"os"

	"github.com/openabstractions/abstraction-identity/go/privatedir"
)

// ownerOnlyBase reports whether p, a per-user base folder such as
// $XDG_RUNTIME_DIR or macOS's $TMPDIR, is a folder of this account open to
// no group or other, found by following links: the test
// enginelaunch.SocketRoot applies before it makes the socket root there
// (inference INF-EL2), owned by identity's privatedir.OwnerOnly.
func ownerOnlyBase(p string) bool {
	info, err := os.Stat(p)
	return err == nil && privatedir.OwnerOnly(info) == nil
}
