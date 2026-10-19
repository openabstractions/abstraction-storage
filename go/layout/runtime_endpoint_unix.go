//go:build unix

package layout

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"

	"github.com/openabstractions/abstraction-identity/go/privatedir"
)

// RuntimeEndpointRoot resolves the account's control-socket namespace without
// creating it. It reads no profile/data roots and is distinct from engine Sockets.
// A supplied invalid XDG_RUNTIME_DIR refuses without a temporary-root fallback.
func RuntimeEndpointRoot() (string, error) {
	uid, euid := os.Getuid(), os.Geteuid()
	if uid != euid {
		return "", fmt.Errorf("layout: mixed real/effective user identity")
	}
	base := os.Getenv("XDG_RUNTIME_DIR")
	temporary := ""
	if base == "" {
		temporary = os.TempDir()
	}
	return runtimeEndpointRoot(uid, euid, base, temporary)
}

func runtimeEndpointRoot(uid, euid int, base, temporary string) (string, error) {
	if uid != euid {
		return "", fmt.Errorf("layout: mixed real/effective user identity")
	}
	var dir string
	if base != "" {
		if !filepath.IsAbs(base) {
			return "", fmt.Errorf("layout: XDG_RUNTIME_DIR is not absolute")
		}
		if err := privatedir.CheckDir(base); err != nil {
			return "", err
		}
		dir = filepath.Join(base, "openabstractions")
	} else {
		dir = filepath.Join(temporary, "openabstractions-"+strconv.Itoa(euid))
	}
	if !filepath.IsAbs(dir) {
		return "", fmt.Errorf("layout: temporary directory is not absolute")
	}
	if _, err := os.Lstat(dir); err == nil {
		if err := privatedir.CheckDir(dir); err != nil {
			return "", err
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return "", err
	}
	return dir, nil
}
