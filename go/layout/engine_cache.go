package layout

import (
	"os"
	"path/filepath"
)

// EngineCacheRoot names the engine folder below an account-data base, the
// installed OpenAbstractions/engines spelling. base is AccountData or a root
// a caller already resolved and admitted; it does not resolve roots, verify
// ownership or migrate this compatibility layout to Roots.Engines.
func EngineCacheRoot(base string) string {
	return filepath.Join(base, "OpenAbstractions", "engines")
}

// EngineCacheDir names an engine copy below an account-data base. This
// preserves the installed OpenAbstractions/engines/<engine>-<release>
// spelling, including its case.
func EngineCacheDir(base, engine, release string) string {
	return filepath.Join(EngineCacheRoot(base), engine+"-"+release)
}

// DefaultProviderStateRoot names the per-account folder that holds each
// file-backed provider store's state folder, OpenAbstractions/providers
// under AccountData (ST-2 ruling 2a). It returns AccountData's refusal.
func DefaultProviderStateRoot() (string, error) {
	return providerStateRoot(Current(), os.UserCacheDir)
}

// DefaultEngineDir names an engine copy under AccountData's
// OpenAbstractions/engines, the folder ModelHost and PyTorch share. It
// returns AccountData's refusal.
func DefaultEngineDir(engine, release string) (string, error) {
	return engineDir(Current(), os.UserCacheDir, engine, release)
}

func providerStateRoot(p Platform, windowsLocal func() (string, error)) (string, error) {
	base, err := accountData(p, windowsLocal)
	if err != nil {
		return "", err
	}
	return join(p.GOOS, base, "OpenAbstractions", "providers"), nil
}

func engineDir(p Platform, windowsLocal func() (string, error), engine, release string) (string, error) {
	base, err := accountData(p, windowsLocal)
	if err != nil {
		return "", err
	}
	return join(p.GOOS, base, "OpenAbstractions", "engines", engine+"-"+release), nil
}
