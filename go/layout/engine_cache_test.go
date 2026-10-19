package layout

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	identity "github.com/openabstractions/abstraction-identity"
)

// A caller must continue reaching an already installed copy, including on a
// case-sensitive filesystem. A new root or a changed version separator would
// strand that copy and its provider-owned metadata.
func TestEngineCacheDirReachesTheExistingInstalledFolder(t *testing.T) {
	cache := t.TempDir()
	for _, engine := range []string{"llama.cpp", "another-engine"} {
		old := filepath.Join(cache, "OpenAbstractions", "engines", engine+"-b10825")
		if err := os.MkdirAll(old, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(old, "installed-copy"), []byte(engine), 0o600); err != nil {
			t.Fatal(err)
		}
		got := EngineCacheDir(cache, engine, "b10825")
		if same, err := identity.SameFile(old, got); err != nil || !same {
			t.Fatalf("existing cache identity: %v %v", same, err)
		}
		body, err := os.ReadFile(filepath.Join(got, "installed-copy"))
		if err != nil || string(body) != engine {
			t.Fatalf("installed copy %q: %v", body, err)
		}
	}
}

func TestDefaultEngineDirFollowsAccountData(t *testing.T) {
	base, wantErr := AccountData()
	got, err := DefaultEngineDir("example-engine", "version")
	if wantErr != nil {
		if err == nil || err.Error() != wantErr.Error() || got != "" {
			t.Fatalf("account data error %v, got %q %v", wantErr, got, err)
		}
		return
	}
	if err != nil || got != EngineCacheDir(base, "example-engine", "version") {
		t.Fatalf("account data %q, got %q %v", base, got, err)
	}
	if runtime.GOOS == "windows" {
		cache, err := os.UserCacheDir()
		if err != nil || base != cache {
			t.Fatalf("windows account data %q, want %%LOCALAPPDATA%% %q %v", base, cache, err)
		}
	}
}
