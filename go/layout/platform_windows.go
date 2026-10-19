//go:build windows

package layout

import (
	"fmt"
	"path/filepath"
	"unsafe"

	"github.com/openabstractions/abstraction-identity/go/profile"
	"golang.org/x/sys/windows"
)

var knownFolders = map[string]*windows.KNOWNFOLDERID{
	"LocalAppData":   windows.FOLDERID_LocalAppData,
	"RoamingAppData": windows.FOLDERID_RoamingAppData,
	"ProgramData":    windows.FOLDERID_ProgramData,
	"Pictures":       windows.FOLDERID_Pictures,
	"Documents":      windows.FOLDERID_Documents,
	"Downloads":      windows.FOLDERID_Downloads,
	"Music":          windows.FOLDERID_Music,
	"Videos":         windows.FOLDERID_Videos,
	"Desktop":        windows.FOLDERID_Desktop,
}

var getTempPath2 = windows.NewLazySystemDLL("kernel32.dll").NewProc("GetTempPath2W")

// Current is this process's Platform: known folders through the process
// token, the temporary folder from GetTempPath2, and the profile view
// probed once by profile.Current when a root is first
// resolved. This package reads none of the LOCALAPPDATA, APPDATA and
// ProgramData variables; the probe still creates its file in the folder the
// LOCALAPPDATA variable names until ST-2 part 1 gives it the known folder
// (scripts/layout_sites.allowlist).
func Current() Platform {
	return Platform{GOOS: "windows", Known: knownFolder, Temp: tempFolder, Profile: profileView}
}

func knownFolder(name string) (string, error) {
	id, ok := knownFolders[name]
	if !ok {
		return "", fmt.Errorf("no known folder is called %q", name)
	}
	return windows.KnownFolderPath(id, windows.KF_FLAG_DEFAULT)
}

func tempFolder() (string, error) {
	buf := make([]uint16, windows.MAX_LONG_PATH)
	var n uint32
	if getTempPath2.Find() == nil {
		r, _, err := getTempPath2.Call(uintptr(len(buf)), uintptr(unsafe.Pointer(&buf[0])))
		if r == 0 {
			return "", err
		}
		n = uint32(r)
	} else {
		// GetTempPath2 arrived in Windows 11 and Server 2022; GetTempPath
		// reads the same variables for a process that is not SYSTEM.
		var err error
		if n, err = windows.GetTempPath(uint32(len(buf)), &buf[0]); err != nil {
			return "", err
		}
	}
	return filepath.Clean(windows.UTF16ToString(buf[:n])), nil
}

func profileView() (string, error) {
	view, err := profile.Current()
	if err != nil {
		return "", err
	}
	if view.Virtualized {
		return view.Family, nil
	}
	return "", nil
}
