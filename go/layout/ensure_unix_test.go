//go:build unix

package layout

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/openabstractions/abstraction-identity/go/privatedir"
)

// [STO-L9] A pre-created ~/.local/share/openabstractions with mode 0755 is
// tightened to 0700.
func TestEnsureTightensAPreCreated0755Root(t *testing.T) {
	root := filepath.Join(t.TempDir(), ".local", "share", "openabstractions")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := Ensure(root, filepath.Join(root, "runtime-v1")); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{root, filepath.Join(root, "runtime-v1")} {
		info, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		if perm := info.Mode().Perm(); perm != 0o700 {
			t.Fatalf("%s has mode %#o, want 0700", p, perm)
		}
	}
}

// notYetPrivate gives p, a folder this test made, mode 0755.
func notYetPrivate(t *testing.T, p string) {
	t.Helper()
	if err := os.Chmod(p, 0o755); err != nil {
		t.Fatal(err)
	}
}

// [STO-L9] A group- or other-writable root is refused: another account may
// already have put something in it.
func TestEnsureRefusesAGroupWritableRoot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "S")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(root, 0o770); err != nil {
		t.Fatal(err)
	}
	if reason := ReasonOf(Ensure(root, root)); reason != OpenToOthers {
		t.Fatalf("reason %q, want %q", reason, OpenToOthers)
	}
}

// [STO-L9] A --state-dir that is a symbolic link, for example to another
// account's folder, is refused.
func TestEnsureRefusesAStateDirLink(t *testing.T) {
	base := t.TempDir()
	target := filepath.Join(base, "elsewhere")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(base, "S")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if reason := ReasonOf(Ensure(link, link)); reason != Link {
		t.Fatalf("reason %q, want %q", reason, Link)
	}
}

// [STO-L9] A root owned by another uid is refused. Only root can make one,
// so an ordinary test judges the entry its own Lstat reports for /, which
// root owns.
func TestEnsureRefusesARootAnotherAccountOwns(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root owns /, so / is this account's own")
	}
	e, err := inspect("/", false)
	if err != nil {
		t.Fatal(err)
	}
	if _, refusal := JudgeOwnerOnly("linux", e); refusal == nil || refusal.Reason != NotOwner {
		t.Fatalf("/ judged %v, want %s (%+v)", refusal, NotOwner, e)
	}
}

// [STO-L9] INF-EL2: the engine socket root is $XDG_RUNTIME_DIR/oa only while
// that folder is this account's and owner-only, as enginelaunch.SocketRoot
// checks; a group-writable base gives /tmp/oa-<uid>. The fallback here is a
// folder of the test's own, so nothing is made in /tmp.
func TestEnsureSocketsJudgesThePerUserBase(t *testing.T) {
	base := t.TempDir()
	run := filepath.Join(base, "run")
	if err := os.Mkdir(run, 0o700); err != nil {
		t.Fatal(err)
	}
	r, err := Resolve(Platform{GOOS: "linux", UID: os.Getuid(), Getenv: func(n string) string {
		return map[string]string{"HOME": base, "XDG_RUNTIME_DIR": run}[n]
	}})
	if err != nil {
		t.Fatal(err)
	}
	if r.Sockets != filepath.Join(run, ShortName) {
		t.Fatalf("Resolve: sockets %s", r.Sockets)
	}
	r.socketsFallback = filepath.Join(base, "tmp", ShortName+"-"+strconv.Itoa(os.Getuid()))
	if err := os.Chmod(run, 0o770); err != nil {
		t.Fatal(err)
	}
	if got, err := EnsureSockets(r); err != nil || got != r.socketsFallback {
		t.Fatalf("a group-writable base: EnsureSockets = %q, %v; want %s", got, err, r.socketsFallback)
	}
	if _, err := os.Lstat(r.Sockets); !os.IsNotExist(err) {
		t.Fatalf("a socket root was made in a group-writable base: %v", err)
	}
	if err := os.Chmod(run, 0o700); err != nil {
		t.Fatal(err)
	}
	if got, err := EnsureSockets(r); err != nil || got != r.Sockets {
		t.Fatalf("an owner-only base: EnsureSockets = %q, %v; want %s", got, err, r.Sockets)
	}
	for _, p := range []string{r.Sockets, r.socketsFallback} {
		if err := privatedir.CheckDir(p); err != nil {
			t.Fatal(err)
		}
	}
}

// [STO-L9] When another account holds /var/tmp/openabstractions-<uid>, tmp
// moves to another owner-only folder instead of failing or sharing it.
func TestEnsureTmpLeavesATakenFolder(t *testing.T) {
	base := t.TempDir()
	taken := filepath.Join(base, "openabstractions-1000")
	if err := os.Mkdir(taken, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(taken, 0o777); err != nil {
		t.Fatal(err)
	}
	run := filepath.Join(base, "run")
	if err := os.Mkdir(run, 0o700); err != nil {
		t.Fatal(err)
	}
	r := Roots{Location: User, GOOS: "linux", Tmp: taken, uid: 1000}
	got, err := EnsureTmp(r, Platform{GOOS: "linux", Getenv: func(n string) string {
		if n == "XDG_RUNTIME_DIR" {
			return run
		}
		return ""
	}})
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(run, Name, "tmp"); got != want {
		t.Fatalf("tmp %s, want %s", got, want)
	}
}
