package layout

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// [STO-L9] [STO-L6] Every root of an isolated location and every child made
// below it is owner-only, and all of them lie under the isolated root.
func TestEnsureMakesAnIsolatedLocationOwnerOnly(t *testing.T) {
	s := filepath.Join(t.TempDir(), "S")
	r, err := ResolveIsolated(runtime.GOOS, s)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := EnsureSockets(r); err != nil || got != r.Sockets {
		t.Fatalf("EnsureSockets = %q, %v; want %s", got, err, r.Sockets)
	}
	for _, dir := range []string{r.Runtime, r.Config, r.Cache, r.Logs, r.Tmp, r.Short, r.Sockets,
		r.ProviderData("example"), r.Engines("example", "1.0"), r.Models(), r.Setup(), r.MCPGateway()} {
		if !strings.HasPrefix(dir, s) {
			t.Fatalf("%s lies outside the isolated root %s", dir, s)
		}
		if err := Ensure(r.Data, dir); err != nil {
			t.Fatalf("Ensure(%s): %v", dir, err)
		}
		for p := dir; ; p = filepath.Dir(p) {
			e, err := inspect(p, false)
			if err != nil {
				t.Fatal(err)
			}
			if action, refusal := JudgeOwnerOnly(runtime.GOOS, e); refusal != nil || action != Keep {
				t.Fatalf("%s after Ensure: %v %v (%+v)", p, action, refusal, e)
			}
			if p == s {
				break
			}
		}
	}
}

// [STO-L9] A file standing where a root belongs is refused, never replaced.
func TestEnsureRefusesAFileInPlaceOfARoot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "openabstractions")
	if err := os.WriteFile(root, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if reason := ReasonOf(Ensure(root, root)); reason != NotDirectory {
		t.Fatalf("reason %q, want %q", reason, NotDirectory)
	}
}

// [STO-L6] [STO-L9] A relative root is refused with not_absolute, and a
// folder outside the root with outside_root.
func TestEnsureRefusesARelativeOrOutsideFolder(t *testing.T) {
	root := filepath.Join(t.TempDir(), "S")
	if reason := ReasonOf(Ensure("S", "S")); reason != NotAbsolute {
		t.Fatalf("relative root: reason %q", reason)
	}
	if reason := ReasonOf(Ensure(root, filepath.Join(filepath.Dir(root), "other"))); reason != OutsideRoot {
		t.Fatalf("outside folder: reason %q", reason)
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatalf("a refused call made %s: %v", root, err)
	}
}

// [STO-L4] A machine file this ordinary process wrote is never trusted, and
// a missing one is absent. A process that is root or an administrator owns
// what it writes as an administrator, so it cannot plant anything and says so.
func TestTrustMachineRefusesAFileThisProcessWrote(t *testing.T) {
	root := filepath.Join(t.TempDir(), "openabstractions")
	file := filepath.Join(root, "config", "config.json")
	if reason := ReasonOf(TrustMachine(file, root)); reason != MachineAbsent {
		t.Fatalf("missing file: reason %q, want %q", reason, MachineAbsent)
	}
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte(`{"store":"planted"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	err := TrustMachine(file, root)
	if err == nil {
		t.Skip("this process writes files as an administrator, so it cannot plant one")
	}
	if reason := ReasonOf(err); reason != MachineUntrusted {
		t.Fatalf("planted file: reason %q, want %q (%v)", reason, MachineUntrusted, err)
	}
	if reason := ReasonOf(TrustMachine(filepath.Join(t.TempDir(), "elsewhere.json"), root)); reason != MachineUntrusted {
		t.Fatalf("a file outside the machine root: reason %q", reason)
	}
}

// [STO-L7] A no-backup folder carries CACHEDIR.TAG with the signature line,
// and a tag already there is kept.
func TestMarkNoBackupWritesTheCacheDirectoryTag(t *testing.T) {
	dir := t.TempDir()
	if err := MarkNoBackup(dir); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "CACHEDIR.TAG"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(raw), "Signature: 8a477f597d28d172789f06886806bc55\n") {
		t.Fatalf("CACHEDIR.TAG starts %q", raw)
	}
	if err := os.WriteFile(filepath.Join(dir, "CACHEDIR.TAG"), []byte(CacheDirTag+"# kept\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := MarkNoBackup(dir); err != nil {
		t.Fatal(err)
	}
	if raw, _ := os.ReadFile(filepath.Join(dir, "CACHEDIR.TAG")); !strings.HasSuffix(string(raw), "# kept\n") {
		t.Fatal("MarkNoBackup replaced a tag that was there")
	}
}
