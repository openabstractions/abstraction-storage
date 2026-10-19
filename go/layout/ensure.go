package layout

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strconv"

	"github.com/openabstractions/abstraction-identity/go/privatedir"
)

// Ensure makes dir and every folder from root down to it owner-only
// (STO-L9). root is a root of a user or isolated location, and dir is root
// or a folder under it. An owner-only folder is a folder private to this
// account as identity CONTRACT.md ID-D1 defines one, and the privatedir
// package makes, checks and tightens it. Each folder is judged by
// JudgeOwnerOnly: a missing one is created with privatedir.Make (mode 0700
// on POSIX; on Windows owned by this account with a protected DACL of this
// account and SYSTEM, set at creation), a private one is kept, and one no
// other account can write is tightened to that descriptor. A link, a file,
// or a folder another account owns or can write is refused with its typed
// reason. The folders above root are platform folders such as
// ~/.local/share; a missing one is created with mode 0700, as the XDG Base
// Directory specification asks, and is not judged.
func Ensure(root, dir string) error {
	if !filepath.IsAbs(root) {
		return refuse(NotAbsolute, root, "a root is an absolute path")
	}
	if !filepath.IsAbs(dir) {
		return refuse(NotAbsolute, dir, "a folder under a root is an absolute path")
	}
	root, dir = filepath.Clean(root), filepath.Clean(dir)
	up, ok := upTo(dir, root)
	if !ok {
		return refuse(OutsideRoot, dir, "it does not lie under the root %s", root)
	}
	if err := os.MkdirAll(filepath.Dir(root), 0o700); err != nil {
		return err
	}
	for i := len(up) - 1; i >= 0; i-- {
		if err := ensureOne(up[i]); err != nil {
			return err
		}
	}
	return nil
}

// inspect reads what the file system says about p. For a root's folder it
// also asks privatedir whether the folder is already private (Entry.Private).
func inspect(p string, machine bool) (Entry, error) {
	e, err := readEntry(p, machine)
	if err == nil && !machine && e.Exists && e.Dir && !e.Link {
		e.Private = privatedir.CheckDir(p) == nil
	}
	return e, err
}

// Inspect reads one path's ownership and access metadata without changing it.
// Callers retaining authority also retain and revalidate its native identity.
func Inspect(path string) (Entry, error) { return inspect(path, false) }

func ensureOne(p string) error {
	e, err := inspect(p, false)
	if err != nil {
		return err
	}
	action, refusal := JudgeOwnerOnly(runtime.GOOS, e)
	if refusal != nil {
		return refusal
	}
	switch action {
	case Keep:
		return nil
	case Create:
		if err := privatedir.Make(p); err != nil && !errors.Is(err, os.ErrExist) {
			return err
		}
	case Tighten:
		if err := privatedir.Tighten(p); err != nil {
			return err
		}
	}
	// Whatever stands there now, created by this call or by another process
	// in between, is judged once more and must be kept as it is.
	if e, err = inspect(p, false); err != nil {
		return err
	}
	action, refusal = JudgeOwnerOnly(runtime.GOOS, e)
	if refusal != nil {
		return refusal
	}
	if action != Keep {
		return refuse(OpenToOthers, p, "it is still not owner-only after %s", action)
	}
	return nil
}

// EnsureTmp makes r's tmp root owner-only and returns it. On macOS and Linux
// another account can pre-create the predictable
// /var/tmp/openabstractions-<uid>; that folder is refused (STO-L9), and the
// tmp root becomes a child of $XDG_RUNTIME_DIR when that folder is this
// account's and owner-only (ownerOnlyBase), otherwise a fresh 0700 folder
// under /var/tmp with a random name (T9 item 6). On Windows, and for an
// isolated location, a refusal stands.
func EnsureTmp(r Roots, p Platform) (string, error) {
	err := Ensure(r.Tmp, r.Tmp)
	if err == nil || r.Location != User || familyOf(r.GOOS) == windowsFamily {
		return r.Tmp, err
	}
	if reason := ReasonOf(err); reason != NotOwner && reason != Link && reason != OpenToOthers && reason != NotDirectory {
		return "", err
	}
	if run := p.env("XDG_RUNTIME_DIR"); filepath.IsAbs(run) && ownerOnlyBase(run) {
		dir := filepath.Join(run, Name, "tmp")
		if err := Ensure(filepath.Join(run, Name), dir); err == nil {
			return dir, nil
		}
	}
	dir, merr := os.MkdirTemp("/var/tmp", Name+"-"+strconv.Itoa(r.uid)+"-")
	if merr != nil {
		return "", errors.Join(err, merr)
	}
	if err := Ensure(dir, dir); err != nil {
		return "", err
	}
	return dir, nil
}

// EnsureSockets makes r's engine socket root owner-only and returns it
// (STO-L9, inference INF-EL2). On macOS and Linux a user location's socket
// root is $TMPDIR/oa or $XDG_RUNTIME_DIR/oa only while that base is a folder
// of this account that no other account can read or write (ownerOnlyBase,
// the check enginelaunch.SocketRoot applies); otherwise it is
// /tmp/oa-<uid>. On Windows and in an isolated location the socket root lies
// under the short root, and Ensure judges it from there down.
func EnsureSockets(r Roots) (string, error) {
	sockets := r.Sockets
	var err error
	switch {
	case r.Location == Isolated:
		err = Ensure(r.Data, sockets)
	case familyOf(r.GOOS) == windowsFamily:
		err = Ensure(r.Short, sockets)
	default:
		if sockets != r.socketsFallback && !ownerOnlyBase(filepath.Dir(sockets)) {
			sockets = r.socketsFallback
		}
		if sockets == "" {
			return "", refuse(NoProfileFolder, "", "no engine socket root was resolved")
		}
		err = Ensure(sockets, sockets)
	}
	if err != nil {
		return "", err
	}
	return sockets, nil
}

func hasOther(list []Holder) bool {
	for _, h := range list {
		if h == Other {
			return true
		}
	}
	return false
}
