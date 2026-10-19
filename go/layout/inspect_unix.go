//go:build unix

package layout

import (
	"errors"
	"io/fs"
	"os"
	"strconv"
	"syscall"
)

// readEntry reads one entry's owner and mode. A machine entry is read through
// links (STO-L4 judges what a reader opens); a root's is not, so a link is
// reported as one (STO-L9).
func readEntry(p string, machine bool) (Entry, error) {
	e := Entry{Path: p}
	stat := os.Lstat
	if machine {
		stat = os.Stat
	}
	info, err := stat(p)
	if errors.Is(err, fs.ErrNotExist) {
		return e, nil
	}
	if err != nil {
		return e, err
	}
	e.Exists = true
	e.Link = info.Mode()&fs.ModeSymlink != 0
	e.Dir = info.IsDir()
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return e, errors.New("its owner cannot be read")
	}
	uid := int(st.Uid)
	e.Mine = uid == os.Getuid()
	switch {
	case uid == 0:
		e.Owner = Admin
	case e.Mine:
		e.Owner = Self
	default:
		e.Owner = Other
	}
	e.OwnerName = "uid " + strconv.Itoa(uid)
	perm := info.Mode().Perm()
	if perm&0o200 != 0 {
		e.Writers = append(e.Writers, e.Owner)
	}
	if perm&0o022 != 0 {
		e.Writers = append(e.Writers, Other)
	} else if perm&0o055 != 0 {
		e.Readers = append(e.Readers, Other)
	}
	return e, nil
}
