package layout

import (
	"os"
	"path/filepath"
)

// SetupConfig returns the existing account Setup folder, shared by the Panel
// and by the runtime that verifies Setup's installed connector images.
func SetupConfig() (string, error) {
	config, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(config, "OpenAbstractions", "setup"), nil
}

// AccountData is the base OA's durable per-account state is spelled under
// (ST-2, architect ruling 2026-11-09). On Windows it is %LOCALAPPDATA% exactly
// as os.UserCacheDir answers it, so every installed path stays byte for byte.
// On macOS and Linux it is Roots.Data of this account's user location
// (~/Library/Application Support/openabstractions,
// $XDG_DATA_HOME/openabstractions), which no cache cleaning removes. It
// refuses as Resolve does (STO-L3) and never falls back to a cache folder.
func AccountData() (string, error) { return accountData(Current(), os.UserCacheDir) }

// RecordsLog is the runtime's records log: %LOCALAPPDATA%\openabstractions\
// logging\records.jsonl on Windows and records.jsonl in Roots.Logs on macOS
// and Linux (~/Library/Logs/openabstractions,
// $XDG_STATE_HOME/log/openabstractions).
func RecordsLog() (string, error) { return recordsLog(Current(), os.UserCacheDir) }

// LocalAppData is the folder where another application keeps its own local
// data, as an application manifest's {local_app_data} names it: the
// LocalAppData known folder on Windows, ~/Library/Application Support on
// macOS and $XDG_DATA_HOME (default ~/.local/share) on Linux.
func LocalAppData() (string, error) { return localAppData(Current()) }

func accountData(p Platform, windowsLocal func() (string, error)) (string, error) {
	if familyOf(p.GOOS) == windowsFamily {
		return windowsLocal()
	}
	r, err := Resolve(p)
	if err != nil {
		return "", err
	}
	return r.Data, nil
}

func recordsLog(p Platform, windowsLocal func() (string, error)) (string, error) {
	if familyOf(p.GOOS) == windowsFamily {
		local, err := windowsLocal()
		if err != nil {
			return "", err
		}
		return join(p.GOOS, local, Name, "logging", "records.jsonl"), nil
	}
	r, err := Resolve(p)
	if err != nil {
		return "", err
	}
	return join(p.GOOS, r.Logs, "records.jsonl"), nil
}

func localAppData(p Platform) (string, error) {
	switch familyOf(p.GOOS) {
	case windowsFamily:
		return known(p, "LocalAppData")
	case darwinFamily:
		home, err := homeOf(p)
		if err != nil {
			return "", err
		}
		return join(p.GOOS, home, "Library", "Application Support"), nil
	case xdgFamily:
		if _, err := homeOf(p); err != nil {
			return "", err
		}
		return xdgBase(p, "XDG_DATA_HOME", ".local", "share")
	}
	return "", refuse(UnsupportedPlatform, "", "no layout for %q", p.GOOS)
}
