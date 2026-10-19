package layout

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"

	"github.com/openabstractions/abstraction-identity/go/privatedir"
)

func TestAccountStateRosterAndPrivateDisjointnessPreserveResetScope(t *testing.T) {
	base := t.TempDir()
	local := filepath.Join(base, "local")
	roaming := filepath.Join(base, "roaming")
	roots := AccountStateRootNames(local, roaming)
	want := []string{filepath.Join(local, Name), filepath.Join(roaming, "abstraction"), filepath.Join(roaming, "OpenAbstractions"), filepath.Join(roaming, "Abstraction Panel")}
	if !reflect.DeepEqual(roots, want) || len(AccountStateRootNames("", "")) != 0 {
		t.Fatal(roots)
	}
	private := filepath.Join(base, "isolated")
	if err := Ensure(private, private); err != nil {
		t.Fatal(err)
	}
	if err := CheckPrivateStatePath(context.Background(), private, roots); err != nil {
		t.Fatal(err)
	}
	for _, root := range roots {
		if err := CheckPrivateStatePath(context.Background(), filepath.Join(root, "child"), roots); !errors.Is(err, ErrAccountStateOverlap) {
			t.Fatalf("installed descendant admitted: %s %v", root, err)
		}
		if _, err := os.Stat(root); !os.IsNotExist(err) {
			t.Fatalf("root construction/check created or opened a store: %s %v", root, err)
		}
	}
	if err := CheckPrivateStatePath(context.Background(), base, roots); !errors.Is(err, ErrAccountStateOverlap) {
		t.Fatal("installed ancestor admitted", err)
	}
	if err := CheckPrivateStatePath(context.Background(), private, []string{"relative"}); err == nil {
		t.Fatal("unresolved account metadata admitted")
	}
	if err := CheckPrivateStatePath(context.Background(), "relative", roots); err == nil {
		t.Fatal("relative private state admitted")
	}
}

// [STO-L9] identity ID-D1 owns what a folder only this account may write is,
// and an owner-only root is one: a folder Ensure creates passes
// privatedir.CheckDir, and a folder privatedir.Ensure creates is kept as it
// is. On Windows the two helpers once wrote different DACLs, and a folder
// Ensure created failed CheckDir with "its DACL admits S-1-5-32-544".
func TestOwnerOnlyFoldersArePrivateFolders(t *testing.T) {
	s := filepath.Join(t.TempDir(), "S")
	r, err := ResolveIsolated(runtime.GOOS, s)
	if err != nil {
		t.Fatal(err)
	}
	if err := Ensure(r.Data, r.Sockets); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{r.Data, r.Short, r.Sockets} {
		if err := privatedir.CheckDir(p); err != nil {
			t.Errorf("a folder Ensure created: %v", err)
		}
	}

	made := filepath.Join(r.Sockets, "launch")
	if err := privatedir.Ensure(made); err != nil {
		t.Fatal(err)
	}
	e, err := inspect(made, false)
	if err != nil {
		t.Fatal(err)
	}
	if action, refusal := JudgeOwnerOnly(runtime.GOOS, e); refusal != nil || action != Keep {
		t.Fatalf("a folder privatedir.Ensure created is judged %q %v, want keep (%+v)", action, refusal, e)
	}
	if err := Ensure(r.Data, made); err != nil {
		t.Fatalf("Ensure over a folder privatedir.Ensure created: %v", err)
	}
	if err := privatedir.CheckDir(made); err != nil {
		t.Fatalf("Ensure changed a private folder: %v", err)
	}
}

// [STO-L9] A folder this account owns that is not yet private, and that no
// other account can write, is tightened to a private folder: mode 0755 on
// POSIX, and on Windows the DACL layout once wrote, which also admits
// Administrators.
func TestEnsureTightensToAPrivateFolder(t *testing.T) {
	root := filepath.Join(t.TempDir(), "S")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	notYetPrivate(t, root)
	if err := privatedir.CheckDir(root); err == nil {
		t.Fatal("the folder is private before Ensure, so nothing is tested")
	}
	if err := Ensure(root, root); err != nil {
		t.Fatal(err)
	}
	if err := privatedir.CheckDir(root); err != nil {
		t.Fatalf("after Ensure: %v", err)
	}
}
