package layout

import (
	"path/filepath"
)

// MachineRoots are the machine location's folders (T9 item 7). The machine
// root holds administrator records only: the machine configuration,
// installer records and the frozen upgrade-v1 folder. The rows of a machine
// service are reserved and empty (TD62), and no secret is written here.
type MachineRoots struct {
	GOOS string
	// Root is M: %ProgramData%\openabstractions,
	// /Library/Application Support/openabstractions or /etc/openabstractions.
	Root string
	// ConfigFile is the machine configuration: M\config\config.json,
	// M/config/config.json, or /etc/openabstractions/config.json.
	ConfigFile string
	// State is reserved for a machine service: M on Windows and macOS,
	// /var/lib/openabstractions on Linux.
	State string
	// OldConfigFile is the 0.4.0 machine configuration
	// (%ProgramData%\abstraction\config.json, /etc/abstraction/config.json),
	// read with the same trust check until an administrator moves it.
	OldConfigFile string
	// UpgradeV1 is the frozen Windows upgrade protocol folder
	// (%ProgramData%\abstraction\upgrade-v1); empty elsewhere.
	UpgradeV1 string
}

// ResolveMachine gives the machine location. It reads the ProgramData known
// folder on Windows and nothing of the account; a sandbox or a virtualized
// profile view refuses, as for the user location.
func ResolveMachine(p Platform) (MachineRoots, error) {
	if err := accountView(p); err != nil {
		return MachineRoots{}, err
	}
	m := MachineRoots{GOOS: p.GOOS}
	switch familyOf(p.GOOS) {
	case windowsFamily:
		data, err := known(p, "ProgramData")
		if err != nil {
			return MachineRoots{}, err
		}
		m.Root = join(p.GOOS, data, Name)
		m.ConfigFile = join(p.GOOS, m.Root, "config", "config.json")
		m.State = m.Root
		m.OldConfigFile = join(p.GOOS, data, "abstraction", "config.json")
		m.UpgradeV1 = join(p.GOOS, data, "abstraction", "upgrade-v1")
	case darwinFamily:
		m.Root = join(p.GOOS, "/Library/Application Support", Name)
		m.ConfigFile = join(p.GOOS, m.Root, "config", "config.json")
		m.State = m.Root
		m.OldConfigFile = "/etc/abstraction/config.json"
	case xdgFamily:
		m.Root = join(p.GOOS, "/etc", Name)
		m.ConfigFile = join(p.GOOS, m.Root, "config.json")
		m.State = join(p.GOOS, "/var/lib", Name)
		m.OldConfigFile = "/etc/abstraction/config.json"
	default:
		return MachineRoots{}, refuse(UnsupportedPlatform, "", "no layout for %q", p.GOOS)
	}
	return m, nil
}

// TrustMachine refuses file unless it and every folder from it up to root
// are owned by an administrator and writable by administrators only
// (STO-L4). root is file's machine root: M, or the folder of an old machine
// file. A file that is missing, or a folder on the way, gives MachineAbsent;
// any other failure gives MachineUntrusted. Links are followed: the entry
// judged is the one a reader would open.
func TrustMachine(file, root string) error {
	file, root = filepath.Clean(file), filepath.Clean(root)
	up, ok := upTo(file, root)
	if !filepath.IsAbs(file) || !ok {
		return refuse(MachineUntrusted, file, "it does not lie under the machine root %s", root)
	}
	var chain []Entry
	for _, p := range up {
		e, err := inspect(p, true)
		if err != nil {
			return refuse(MachineUntrusted, p, "who owns it cannot be read: %v", err)
		}
		chain = append(chain, e)
	}
	if r := JudgeMachine(chain); r != nil {
		return r
	}
	return nil
}

// upTo is p and each folder above it up to root, p first and root last, when
// root is p or one of the folders above it; both are cleaned paths. It
// compares spellings exactly and asks nothing of the file system: root and
// p are names layout or its caller spelled, and a root spelled another way
// is another root (VISION.md L623; identity's Within answers by file
// identity).
func upTo(p, root string) ([]string, bool) {
	var up []string
	for {
		up = append(up, p)
		if p == root {
			return up, true
		}
		parent := filepath.Dir(p)
		if parent == p {
			return nil, false
		}
		p = parent
	}
}
