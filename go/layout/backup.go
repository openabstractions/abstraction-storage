package layout

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
)

// CacheDirTag is the CACHEDIR.TAG a no-backup folder carries; its first line
// is the signature https://bford.info/cachedir/ defines, which borg, restic
// and GNU tar honour.
const CacheDirTag = "Signature: 8a477f597d28d172789f06886806bc55\n" +
	"# This file is a cache directory tag created by OpenAbstractions.\n" +
	"# For information about cache directory tags, see https://bford.info/cachedir/\n"

// MarkNoBackup gives dir, an existing folder, the platform's no-backup mark
// (STO-L7): CACHEDIR.TAG on every platform, and on macOS the sticky Time
// Machine exclusion NSURLIsExcludedFromBackupKey sets. A tag already there
// is kept.
func MarkNoBackup(dir string) error {
	tag := filepath.Join(dir, "CACHEDIR.TAG")
	f, err := os.OpenFile(tag, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	switch {
	case errors.Is(err, fs.ErrExist):
	case err != nil:
		return err
	default:
		_, werr := f.WriteString(CacheDirTag)
		if err := errors.Join(werr, f.Close()); err != nil {
			return err
		}
	}
	return excludeFromTimeMachine(dir)
}

// EnsureNoBackup makes dir, owner-only when it is new, and on macOS and Linux
// gives it MarkNoBackup: the folders under AccountData that hold engines,
// downloaded application copies and model stages, each downloaded again by digest
// (ST-2 ruling 4). Windows folders keep their state.
func EnsureNoBackup(dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if runtime.GOOS == "windows" {
		return nil
	}
	return MarkNoBackup(dir)
}
