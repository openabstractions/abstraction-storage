package layout

import (
	"bufio"
	"bytes"
	"os"
	"strings"
)

// ProcessHome returns the home directory as seen by the current process.
// Use it for external configuration that follows the process environment.
// Callers that require profile admission resolve that separately.
func ProcessHome() (string, error) { return os.UserHomeDir() }

// PersonFolder is one of the person's own folders, a user-content known
// folder. OA writes into one only at a destination a caller named (STO-L5;
// download DL-R20, DL-R30); ResolvePerson tells a caller where it is, for
// example to offer it as a destination.
type PersonFolder string

const (
	Pictures  PersonFolder = "pictures"
	Documents PersonFolder = "documents"
	Downloads PersonFolder = "downloads"
	Music     PersonFolder = "music"
	Videos    PersonFolder = "videos"
	Desktop   PersonFolder = "desktop"
)

var personFolders = map[PersonFolder]struct{ known, xdg, home, mac string }{
	Pictures:  {"Pictures", "XDG_PICTURES_DIR", "Pictures", "Pictures"},
	Documents: {"Documents", "XDG_DOCUMENTS_DIR", "Documents", "Documents"},
	Downloads: {"Downloads", "XDG_DOWNLOAD_DIR", "Downloads", "Downloads"},
	Music:     {"Music", "XDG_MUSIC_DIR", "Music", "Music"},
	Videos:    {"Videos", "XDG_VIDEOS_DIR", "Videos", "Movies"},
	Desktop:   {"Desktop", "XDG_DESKTOP_DIR", "Desktop", "Desktop"},
}

// ResolvePerson gives a person's folder: the Windows known folder read
// through the token; on macOS the folder of that name in $HOME; on Linux
// the XDG user-dirs entry ($XDG_CONFIG_HOME/user-dirs.dirs), otherwise the
// folder of that name in $HOME. An entry that names $HOME itself (the
// specification's way to switch a folder off) is refused, as is one that is
// not absolute.
func ResolvePerson(p Platform, f PersonFolder) (string, error) {
	names, ok := personFolders[f]
	if !ok {
		return "", refuse(NoProfileFolder, "", "no person's folder is called %q", f)
	}
	switch familyOf(p.GOOS) {
	case windowsFamily:
		return known(p, names.known)
	case darwinFamily:
		home, err := homeOf(p)
		if err != nil {
			return "", err
		}
		return join(p.GOOS, home, names.mac), nil
	case xdgFamily:
		home, err := homeOf(p)
		if err != nil {
			return "", err
		}
		dir := join(p.GOOS, home, names.home)
		if v, ok := userDir(p, home, names.xdg); ok {
			dir = v
		}
		if !isAbs(p.GOOS, dir) {
			return "", refuse(NoProfileFolder, dir, "the %s folder is not absolute", f)
		}
		if dir == home {
			return "", refuse(NoProfileFolder, dir, "the %s folder is switched off: it names the home folder", f)
		}
		return dir, nil
	}
	return "", refuse(UnsupportedPlatform, "", "no layout for %q", p.GOOS)
}

// userDir reads one entry of user-dirs.dirs: KEY="$HOME/rest" or
// KEY="/absolute", the two forms the specification allows.
func userDir(p Platform, home, key string) (string, bool) {
	if p.ReadFile == nil {
		return "", false
	}
	config, err := xdgBase(p, "XDG_CONFIG_HOME", ".config")
	if err != nil {
		return "", false
	}
	raw, err := p.ReadFile(join(p.GOOS, config, "user-dirs.dirs"))
	if err != nil {
		return "", false
	}
	sc := bufio.NewScanner(bytes.NewReader(raw))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		name, value, ok := strings.Cut(line, "=")
		if !ok || strings.HasPrefix(line, "#") || strings.TrimSpace(name) != key {
			continue
		}
		value = strings.Trim(strings.TrimSpace(value), `"`)
		if rest, ok := strings.CutPrefix(value, "$HOME"); ok {
			return join(p.GOOS, home, rest), true
		}
		return value, true
	}
	return "", false
}

// PersonProfileVariables are the Windows variables naming the person's roaming
// and local application data and profile folders. A clean child environment
// asks its builder to compute exactly these from the account's token and
// registry; layout owns their spelling.
func PersonProfileVariables() []string {
	return []string{"APPDATA", "LOCALAPPDATA", "USERPROFILE"}
}
