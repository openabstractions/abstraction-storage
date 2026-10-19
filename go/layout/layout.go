// Package layout is the one owner of where OpenAbstractions keeps files on a
// device (storage CONTRACT.md, section "Locations", STO-L1 to STO-L9).
//
// A root is named by its location and its class: the location is `machine`
// (one per device), `user` (one per account) or `isolated` (one runtime
// started with an explicit root, such as --state-dir, a test or a
// container), and the class is one of the four retention classes of STO-T1
// (`data`, `cache`, `tmp`, `logs`) or a record kind with its own root
// (`config`, path-bound kinds such as engine sockets). Every root is
// computed here from a Platform, a value that carries the operating system's
// answers: known folders read through the token on Windows, `$HOME` and the
// XDG variables elsewhere. Resolve, ResolveIsolated, ResolveMachine and
// ResolvePerson are pure functions of that value, so a test injects a
// Platform and never reaches the account's real folders; Current reads the
// running process's.
//
// A root that cannot be resolved is a *Refusal with a typed Reason (STO-L3).
// Nothing here substitutes the temporary folder, the home folder or another
// location. Ensure creates a root and its children owner-only and refuses a
// root another account owns or can write (STO-L9); TrustMachine is the
// administrator check a machine file passes before anyone reads it (STO-L4).
package layout

import (
	"errors"
	"fmt"
	"path"
	"strconv"
	"strings"
	"unicode/utf16"
)

// Location is where a root lives: one per device, one per account, or one
// per runtime started with an explicit root (research/vocabulary/DECISION.md
// D20 names the word; storage's values are these three).
type Location string

const (
	Machine  Location = "machine"
	User     Location = "user"
	Isolated Location = "isolated"
)

// Name is the one folder name of every OA data, config, cache, log and tmp
// folder on every platform (T9 item 3).
const Name = "openabstractions"

// WindowsAccountFolders resolves native account-folder metadata without probing
// the profile view. Mutation callers must separately admit that view before
// changing account state; metadata resolution itself creates nothing.
func WindowsAccountFolders(p Platform) (local, roaming string, err error) {
	if familyOf(p.GOOS) != windowsFamily {
		return "", "", refuse(UnsupportedPlatform, "", "Windows account folders require Windows")
	}
	local, err = known(p, "LocalAppData")
	if err != nil {
		return "", "", err
	}
	roaming, err = known(p, "RoamingAppData")
	if err != nil {
		return "", "", err
	}
	return local, roaming, nil
}

// ShortName is the short root for kinds whose contract names a path-length
// limit (T9 item 3).
const ShortName = "oa"

// Reason is a refusal's typed reason. Its values are the words a refusal
// carries over every API that reports one.
type Reason string

const (
	// NoProfileFolder: a known folder, $HOME or a person's folder cannot be
	// read, or is not an absolute path.
	NoProfileFolder Reason = "no_profile_folder"
	// VirtualizedProfile: on Windows this process sees a packaged app's
	// private copy of its profile folders.
	VirtualizedProfile Reason = "virtualized_profile"
	// UnknownProfileView: this process's profile view could not be read.
	UnknownProfileView Reason = "unknown_profile_view"
	// Sandboxed: this process runs inside an App Sandbox container, a
	// Flatpak or a Snap.
	Sandboxed Reason = "sandboxed"
	// NotAbsolute: an isolated root, or a folder handed to Ensure, is not an
	// absolute path. On Windows an absolute path is a drive letter, a colon
	// and a separator, or a UNC path beginning with two backslashes; `\x`
	// and `C:x` are relative.
	NotAbsolute Reason = "not_absolute"
	// OutsideRoot: a folder handed to Ensure does not lie under the root it
	// is ensured from, or an element given to PathBound is not one folder
	// name below the kind's folder.
	OutsideRoot Reason = "outside_root"
	// PathTooLong: a computed path exceeds its kind's limit (STO-L6).
	PathTooLong Reason = "path_too_long"
	// NotOwner: an existing root or child is owned by another account
	// (STO-L9).
	NotOwner Reason = "not_owner"
	// OpenToOthers: an existing root or child is writable by another account
	// (STO-L9).
	OpenToOthers Reason = "open_to_others"
	// Link: an existing root or child is itself a symbolic link or a
	// junction (STO-L9). Ensure judges a root and the folders below it, so a
	// link above the root, such as macOS's /var, is followed.
	Link Reason = "link"
	// NotDirectory: an existing root or child is not a folder.
	NotDirectory Reason = "not_directory"
	// MachineAbsent: no machine root, or no machine file, exists (STO-L4).
	MachineAbsent Reason = "machine_absent"
	// MachineUntrusted: a machine file or a folder above it is owned or
	// writable by someone other than an administrator (STO-L4).
	MachineUntrusted Reason = "machine_untrusted"
	// UnsupportedPlatform: this operating system has no layout.
	UnsupportedPlatform Reason = "unsupported_platform"
)

// Refusal is the typed refusal every function of this package returns when
// it cannot give a root. Path names the folder or file concerned when there
// is one.
type Refusal struct {
	Reason Reason
	Path   string
	Detail string
}

func (r *Refusal) Error() string {
	var b strings.Builder
	b.WriteString("layout: ")
	b.WriteString(string(r.Reason))
	if r.Path != "" {
		b.WriteString(": ")
		b.WriteString(r.Path)
	}
	if r.Detail != "" {
		b.WriteString(": ")
		b.WriteString(r.Detail)
	}
	return b.String()
}

// ReasonOf is the typed reason of a refusal anywhere in err's chain, or ""
// when err is no Refusal.
func ReasonOf(err error) Reason {
	var r *Refusal
	if errors.As(err, &r) {
		return r.Reason
	}
	return ""
}

func refuse(reason Reason, p, detail string, args ...any) *Refusal {
	return &Refusal{Reason: reason, Path: p, Detail: fmt.Sprintf(detail, args...)}
}

// Platform is what the operating system answers about this account. Current
// fills it for the running process; tests construct it with fakes.
type Platform struct {
	// GOOS is the operating system, as runtime.GOOS spells it.
	GOOS string
	// Known answers a Windows known folder by name ("LocalAppData", "RoamingAppData",
	// "ProgramData", "Pictures", "Documents", "Downloads", "Music",
	// "Videos", "Desktop"), read through the process token.
	Known func(name string) (string, error)
	// Temp answers the Windows per-user temporary folder (GetTempPath2,
	// which reads TMP, TEMP and USERPROFILE).
	Temp func() (string, error)
	// Profile answers this process's Windows profile view: the package
	// family whose private copy of the profile it sees, or "" for the real
	// view.
	Profile func() (family string, err error)
	// Getenv answers $HOME, the XDG variables and $TMPDIR on macOS and
	// Linux.
	Getenv func(name string) string
	// ReadFile reads the XDG user-dirs file for a person's folder on Linux.
	ReadFile func(name string) ([]byte, error)
	// UID is this account's user id on macOS and Linux.
	UID int
	// Sandbox names the sandbox this process runs in ("app-sandbox",
	// "flatpak", "snap"), or "" for none.
	Sandbox string
}

func (p Platform) env(name string) string {
	if p.Getenv == nil {
		return ""
	}
	return p.Getenv(name)
}

// family is how an operating system lays out its folders.
type family int

const (
	unsupported family = iota
	windowsFamily
	darwinFamily
	xdgFamily
)

func familyOf(goos string) family {
	switch goos {
	case "windows":
		return windowsFamily
	case "darwin", "ios":
		return darwinFamily
	case "linux", "android", "freebsd", "openbsd", "netbsd", "dragonfly", "solaris", "illumos", "aix":
		return xdgFamily
	}
	return unsupported
}

// Roots are the folders of one user or isolated location. Every path is
// absolute and spelled for Platform.GOOS.
type Roots struct {
	Location Location
	GOOS     string
	// Data is D: the user data root, or the isolated root S itself.
	Data string
	// Runtime is the runtime state folder: D/runtime-v1, or S (T9 item 4).
	Runtime string
	// Config holds the user configuration record.
	Config string
	// Cache is the cache root; it carries CACHEDIR.TAG (STO-L7).
	Cache string
	// Logs is the logs root.
	Logs string
	// Tmp is the root for small transient files that never become data.
	Tmp string
	// Short is the base of path-bound kinds on Windows and in an isolated
	// root (%LOCALAPPDATA%\oa, S/oa). On macOS and Linux a user location has
	// none, and PathBound places those kinds under D.
	Short string
	// Sockets is the engine socket root: %LOCALAPPDATA%\oa\s, S/oa/s,
	// $XDG_RUNTIME_DIR/oa or $TMPDIR/oa, otherwise /tmp/oa-<uid>. Resolve
	// reads the variable only; EnsureSockets checks that the per-user base is
	// this account's and owner-only, and otherwise takes /tmp/oa-<uid>.
	Sockets string
	// Install holds a per-user installer's bookkeeping on macOS and Linux;
	// empty for an isolated location.
	Install string
	// UpgradeV1 is the frozen Windows upgrade protocol folder; empty
	// elsewhere and for an isolated location.
	UpgradeV1 string
	// uid names the Linux tmp fallback (EnsureTmp).
	uid int
	// socketsFallback is /tmp/oa-<uid> for a user location on macOS and
	// Linux (EnsureSockets).
	socketsFallback string
}

// ProviderData is a provider's durable state folder.
func (r Roots) ProviderData(provider string) string {
	return join(r.GOOS, r.Data, "provider-data", provider)
}

// Engines is a component a provider or Setup installs, by provider and
// version; it carries the no-backup mark (STO-L7).
func (r Roots) Engines(provider, version string) string {
	return join(r.GOOS, r.Data, "engines", provider, version)
}

// Models is the root of OA's own model sets; it carries the no-backup mark
// when a set's source can fetch it again by digest (STO-L7).
func (r Roots) Models() string { return join(r.GOOS, r.Data, "models") }

// Setup holds the Panel's Setup journals and receipts.
func (r Roots) Setup() string { return join(r.GOOS, r.Data, "setup") }

// MCPGateway holds the gateway's job handles.
func (r Roots) MCPGateway() string { return join(r.GOOS, r.Data, "mcp-gateway") }

// ConfigFile is the user configuration record.
func (r Roots) ConfigFile() string { return join(r.GOOS, r.Config, "config.json") }

// Kind is a path-bound kind: a folder whose deepest path some program opens
// under a stated limit, so it lives under the short root (T9 item 3).
type Kind struct {
	// Name is the kind's child of the short root.
	Name string
	// Limit is the longest path the kind allows, in Units.
	Limit int
	// Reserve is the part of Limit taken below the folder a caller names.
	Reserve int
	// Bytes counts Limit in bytes; otherwise in UTF-16 code units.
	Bytes bool
	// WindowsOnly applies Limit on Windows only.
	WindowsOnly bool
	// child is where the kind lives under D on macOS and Linux.
	child string
}

var (
	// EngineSockets holds engine launch folders: a socket path
	// <folder>/<8 hex digits>/e.sock fits sockaddr_un's 108 bytes with its
	// terminator (enginelaunch.MaxSocketPath = 107).
	EngineSockets = Kind{Name: "s", Limit: 107, Reserve: len("/12345678/e.sock"), Bytes: true}
	// GPUEngines holds engines whose GPU libraries open their files without
	// Windows long-path support; the folder holding the engine's launcher is
	// at most 87 UTF-16 code units (thenoise.MaxEngineDirLength).
	GPUEngines = Kind{Name: "e", Limit: 87, WindowsOnly: true, child: "engines"}
	// GPUProviderState holds the state of those providers, whose engine
	// kernel caches are redirected into it.
	GPUProviderState = Kind{Name: "p", Limit: 87, WindowsOnly: true, child: "provider-data"}
	// ModelStages holds Setup's model stages for those engines (T9 item 12).
	ModelStages = Kind{Name: "m", Limit: 87, WindowsOnly: true, child: "models"}
)

// PathBound is a path of a path-bound kind: under the short root on Windows
// and in an isolated root, under D on macOS and Linux for a user location;
// the engine socket kind is always the Sockets root. Each of elems is one
// folder name below the kind's folder; an element that is empty or made
// only of dots, or holds a separator (and on Windows a colon), is refused
// with OutsideRoot. It refuses with PathTooLong when the path, with the kind's
// reserve, exceeds the kind's limit (STO-L6); a path at the limit fits.
func (r Roots) PathBound(k Kind, elems ...string) (string, error) {
	for _, e := range elems {
		if strings.Trim(e, ".") == "" || strings.Contains(e, "/") ||
			(r.GOOS == "windows" && strings.ContainsAny(e, `\:`)) {
			return "", refuse(OutsideRoot, e, "an element below the %s kind's folder is one folder name", k.Name)
		}
	}
	var base string
	switch {
	case k == EngineSockets:
		base = r.Sockets
	case r.Short != "":
		base = join(r.GOOS, r.Short, k.Name)
	default:
		base = join(r.GOOS, r.Data, k.child)
	}
	p := join(r.GOOS, append([]string{base}, elems...)...)
	if k.WindowsOnly && r.GOOS != "windows" {
		return p, nil
	}
	n := len(p)
	if !k.Bytes {
		n = len(utf16.Encode([]rune(p)))
	}
	if n+k.Reserve > k.Limit {
		unit := "UTF-16 code units"
		if k.Bytes {
			unit = "bytes"
		}
		return "", refuse(PathTooLong, p, "%d %s with %d reserved below it exceeds the %s kind's limit of %d; use a shorter root",
			n, unit, k.Reserve, k.Name, k.Limit)
	}
	return p, nil
}

// Resolve gives this account's user location (T9 items 5 and 6). A process
// in a sandbox or a virtualized profile view is refused, as is a profile
// folder that cannot be read or is not absolute.
func Resolve(p Platform) (Roots, error) {
	if err := accountView(p); err != nil {
		return Roots{}, err
	}
	r := Roots{Location: User, GOOS: p.GOOS, uid: p.UID}
	switch familyOf(p.GOOS) {
	case windowsFamily:
		local, err := known(p, "LocalAppData")
		if err != nil {
			return Roots{}, err
		}
		temp, err := windowsTemp(p)
		if err != nil {
			return Roots{}, err
		}
		r.Data = join(p.GOOS, local, Name)
		r.Config = join(p.GOOS, r.Data, "config")
		r.Cache = join(p.GOOS, r.Data, "cache")
		r.Logs = join(p.GOOS, r.Data, "logs")
		r.Tmp = join(p.GOOS, temp, Name)
		r.Short = join(p.GOOS, local, ShortName)
		r.Sockets = join(p.GOOS, r.Short, EngineSockets.Name)
		r.UpgradeV1 = join(p.GOOS, r.Data, "upgrade-v1")
	case darwinFamily:
		home, err := homeOf(p)
		if err != nil {
			return Roots{}, err
		}
		library := join(p.GOOS, home, "Library")
		r.Data = join(p.GOOS, library, "Application Support", Name)
		r.Config = join(p.GOOS, r.Data, "config")
		r.Cache = join(p.GOOS, library, "Caches", Name)
		r.Logs = join(p.GOOS, library, "Logs", Name)
		r.Tmp = posixTmp(p, false)
		r.Sockets, r.socketsFallback = posixSockets(p, "TMPDIR")
		r.Install = join(p.GOOS, r.Data, "install")
	case xdgFamily:
		// $HOME is required even when every XDG variable is absolute: a
		// process without a home folder has no account location (STO-L3).
		if _, err := homeOf(p); err != nil {
			return Roots{}, err
		}
		var err error
		if r.Data, err = xdg(p, "XDG_DATA_HOME", ".local", "share"); err != nil {
			return Roots{}, err
		}
		if r.Config, err = xdg(p, "XDG_CONFIG_HOME", ".config"); err != nil {
			return Roots{}, err
		}
		if r.Cache, err = xdg(p, "XDG_CACHE_HOME", ".cache"); err != nil {
			return Roots{}, err
		}
		state, err := xdgBase(p, "XDG_STATE_HOME", ".local", "state")
		if err != nil {
			return Roots{}, err
		}
		r.Logs = join(p.GOOS, state, "log", Name)
		r.Tmp = posixTmp(p, true)
		r.Sockets, r.socketsFallback = posixSockets(p, "XDG_RUNTIME_DIR")
		r.Install = join(p.GOOS, r.Data, "install")
	default:
		return Roots{}, refuse(UnsupportedPlatform, "", "no layout for %q", p.GOOS)
	}
	r.Runtime = join(p.GOOS, r.Data, "runtime-v1")
	return r, nil
}

// ResolveIsolated gives the isolated location of a runtime started with root
// s: s is the runtime state folder and every other root is a reserved child
// of it (T9 item 6, STO-L6). It reads nothing of the account, so it needs
// only the operating system's name; a sandbox or a virtualized view does not
// refuse it, since s is named rather than found.
func ResolveIsolated(goos, s string) (Roots, error) {
	if familyOf(goos) == unsupported {
		return Roots{}, refuse(UnsupportedPlatform, "", "no layout for %q", goos)
	}
	if !isAbs(goos, s) {
		return Roots{}, refuse(NotAbsolute, s, "an isolated root is an absolute path")
	}
	s = join(goos, s)
	r := Roots{Location: Isolated, GOOS: goos, Data: s, Runtime: s}
	r.Config = join(goos, s, "config")
	r.Cache = join(goos, s, "cache")
	r.Logs = join(goos, s, "logs")
	r.Tmp = join(goos, s, "tmp")
	r.Short = join(goos, s, ShortName)
	r.Sockets = join(goos, r.Short, EngineSockets.Name)
	return r, nil
}

// Classify names the root of r a path lies under, the most specific one, or
// "" when it lies under none: a destination a caller or an operator named,
// such as a download destination, the NAS store or a folder adopted in
// place, lies outside STO-L1 and follows its owning contract. It compares
// the cleaned spellings exactly, with no case folded: it answers where
// layout's own spellings place a path, and whether an existing file lies in
// a root is a question for the file system (identity's Within).
func (r Roots) Classify(p string) string {
	named := []struct{ name, root string }{
		{"sockets", r.Sockets}, {"short", r.Short}, {"runtime", r.Runtime},
		{"config", r.Config}, {"cache", r.Cache}, {"logs", r.Logs}, {"tmp", r.Tmp},
		{"data", r.Data},
	}
	best, length := "", -1
	for _, n := range named {
		if n.root == "" || !under(r.GOOS, p, n.root) {
			continue
		}
		if len(n.root) > length {
			best, length = n.name, len(n.root)
		}
	}
	return best
}

func accountView(p Platform) error {
	if p.Sandbox != "" {
		return refuse(Sandboxed, "", "this process runs in %s, whose folders are not the account's", p.Sandbox)
	}
	if familyOf(p.GOOS) != windowsFamily {
		return nil
	}
	if p.Profile == nil {
		return refuse(UnknownProfileView, "", "no profile view was given")
	}
	family, err := p.Profile()
	if err != nil {
		return refuse(UnknownProfileView, "", "%v", err)
	}
	if family != "" {
		return refuse(VirtualizedProfile, "", "this process sees the private profile copy of the packaged app %s", family)
	}
	return nil
}

func known(p Platform, name string) (string, error) {
	if p.Known == nil {
		return "", refuse(NoProfileFolder, "", "the known folder %s cannot be read here", name)
	}
	v, err := p.Known(name)
	if err != nil {
		return "", refuse(NoProfileFolder, "", "the known folder %s: %v", name, err)
	}
	if !isAbs(p.GOOS, v) {
		return "", refuse(NoProfileFolder, v, "the known folder %s is not absolute", name)
	}
	return join(p.GOOS, v), nil
}

func windowsTemp(p Platform) (string, error) {
	if p.Temp == nil {
		return "", refuse(NoProfileFolder, "", "the temporary folder cannot be read here")
	}
	v, err := p.Temp()
	if err != nil {
		return "", refuse(NoProfileFolder, "", "the temporary folder: %v", err)
	}
	if !isAbs(p.GOOS, v) {
		return "", refuse(NoProfileFolder, v, "the temporary folder is not absolute")
	}
	return join(p.GOOS, v), nil
}

func homeOf(p Platform) (string, error) {
	h := p.env("HOME")
	if h == "" {
		return "", refuse(NoProfileFolder, "", "HOME is not set")
	}
	if !isAbs(p.GOOS, h) {
		return "", refuse(NoProfileFolder, h, "HOME is not absolute")
	}
	return join(p.GOOS, h), nil
}

// xdgBase is an XDG base folder: the variable when it holds an absolute
// path, otherwise the specification's default under $HOME (a relative or
// empty value is ignored, as the XDG Base Directory specification says).
func xdgBase(p Platform, variable string, fallback ...string) (string, error) {
	if v := p.env(variable); isAbs(p.GOOS, v) {
		return join(p.GOOS, v), nil
	}
	home, err := homeOf(p)
	if err != nil {
		return "", err
	}
	return join(p.GOOS, append([]string{home}, fallback...)...), nil
}

func xdg(p Platform, variable string, fallback ...string) (string, error) {
	base, err := xdgBase(p, variable, fallback...)
	if err != nil {
		return "", err
	}
	return join(p.GOOS, base, Name), nil
}

// posixTmp is a folder of $TMPDIR when $TMPDIR is absolute (macOS's is per
// user, so its name carries no uid there), otherwise
// /var/tmp/openabstractions-<uid>; EnsureTmp takes another folder when
// another account holds that one.
func posixTmp(p Platform, uidInName bool) string {
	withUID := Name + "-" + strconv.Itoa(p.UID)
	if v := p.env("TMPDIR"); isAbs(p.GOOS, v) {
		if uidInName {
			return join(p.GOOS, v, withUID)
		}
		return join(p.GOOS, v, Name)
	}
	return join(p.GOOS, "/var/tmp", withUID)
}

// posixSockets is <variable>/oa when the variable is absolute, otherwise
// /tmp/oa-<uid>, and the /tmp/oa-<uid> fallback EnsureSockets takes when the
// variable's folder is not this account's and owner-only (INF-EL2).
func posixSockets(p Platform, variable string) (sockets, fallback string) {
	fallback = join(p.GOOS, "/tmp", ShortName+"-"+strconv.Itoa(p.UID))
	if v := p.env(variable); isAbs(p.GOOS, v) {
		return join(p.GOOS, v, ShortName), fallback
	}
	return fallback, fallback
}

// isAbs answers for goos, whatever this process runs on.
func isAbs(goos, p string) bool {
	if goos == "windows" {
		if strings.HasPrefix(p, `\\`) {
			return len(p) > 2
		}
		return len(p) >= 3 && p[1] == ':' && (p[2] == '\\' || p[2] == '/') &&
			(('a' <= p[0] && p[0] <= 'z') || ('A' <= p[0] && p[0] <= 'Z'))
	}
	return strings.HasPrefix(p, "/")
}

// join joins and cleans for goos, whatever this process runs on.
func join(goos string, elems ...string) string {
	if goos != "windows" {
		return path.Join(elems...)
	}
	prefix := ""
	parts := make([]string, len(elems))
	for i, e := range elems {
		parts[i] = strings.ReplaceAll(e, `\`, "/")
	}
	if len(parts) > 0 && strings.HasPrefix(parts[0], "//") {
		prefix, parts[0] = `\\`, strings.TrimLeft(parts[0], "/")
	}
	joined := path.Join(parts...)
	if len(joined) == 2 && joined[1] == ':' {
		joined += "/"
	}
	return prefix + strings.ReplaceAll(joined, "/", `\`)
}

// under reports whether the spelling p is the spelling root or lies below
// it, both cleaned for goos; no case is folded.
func under(goos, p, root string) bool {
	if !isAbs(goos, p) {
		return false
	}
	p, root = join(goos, p), join(goos, root)
	sep := "/"
	if goos == "windows" {
		sep = `\`
	}
	return p == root || strings.HasPrefix(p, strings.TrimSuffix(root, sep)+sep)
}
