//go:build windows

package layout

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"
)

// [STO-L9] A root reached through a junction is refused. mklink /J needs no
// privilege, so the junction is real.
func TestEnsureRefusesAJunction(t *testing.T) {
	base := t.TempDir()
	target := filepath.Join(base, "elsewhere")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(base, "S")
	if out, err := exec.Command("cmd", "/c", "mklink", "/J", link, target).CombinedOutput(); err != nil {
		t.Fatalf("mklink /J: %v: %s", err, out)
	}
	if reason := ReasonOf(Ensure(link, link)); reason != Link {
		t.Fatalf("reason %q, want %q", reason, Link)
	}
	if err := CheckPrivateStatePath(context.Background(), link, []string{target}); err == nil {
		t.Fatal("linked account-state alias admitted")
	}
}

// [STO-L9] A root Everyone may read is tightened to a private folder of this
// account and SYSTEM (identity ID-D1); one Everyone may write is refused.
func TestEnsureTightensAndRefusesByTheDACL(t *testing.T) {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	me := user.User.Sid.String()
	for _, c := range []struct {
		name, everyone string
		want           Reason
	}{
		{"read", "FR", ""},
		{"write", "FA", OpenToOthers},
	} {
		t.Run(c.name, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "S")
			if err := os.Mkdir(root, 0o700); err != nil {
				t.Fatal(err)
			}
			setDACL(t, root, "D:P(A;OICI;FA;;;"+me+")(A;OICI;"+c.everyone+";;;WD)")
			err := Ensure(root, root)
			if reason := ReasonOf(err); reason != c.want {
				t.Fatalf("reason %q, want %q (%v)", reason, c.want, err)
			}
			if c.want != "" {
				return
			}
			e, err := inspect(root, false)
			if err != nil {
				t.Fatal(err)
			}
			if len(e.Readers) != 0 {
				t.Fatalf("after tightening others still read: %+v", e)
			}
		})
	}
}

// notYetPrivate gives p, a folder this test made, the protected DACL layout
// wrote before it took identity ID-D1's: this account, SYSTEM and
// Administrators.
func notYetPrivate(t *testing.T, p string) {
	t.Helper()
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	me := user.User.Sid.String()
	setDACL(t, p, "D:P(A;OICI;FA;;;"+me+")(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)")
}

func setDACL(t *testing.T, p, sddl string) {
	t.Helper()
	sd, err := windows.SecurityDescriptorFromString(sddl)
	if err != nil {
		t.Fatal(err)
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		t.Fatal(err)
	}
	if err := windows.SetNamedSecurityInfo(p, windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, dacl, nil); err != nil {
		t.Fatal(err)
	}
}

// [STO-L4] On Windows an ordinary account may add new entries to a machine
// folder that inherits %ProgramData%'s DACL; the folder still passes, since
// every file read is checked itself, while a folder Everyone may delete from
// does not. Both folders are this test's own, so ownership is judged apart.
func TestMachineFolderAddRightsAreNotWrites(t *testing.T) {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	me := user.User.Sid.String()
	add := filepath.Join(t.TempDir(), "add")
	del := filepath.Join(t.TempDir(), "delete")
	for _, d := range []string{add, del} {
		if err := os.Mkdir(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	// Users may add files and subfolders (0x6), as %ProgramData% grants.
	setDACL(t, add, "D:P(A;;FA;;;"+me+")(A;;0x6;;;BU)")
	// Everyone may delete what the folder holds (FILE_DELETE_CHILD, 0x40).
	setDACL(t, del, "D:P(A;;FA;;;"+me+")(A;;0x40;;;WD)")
	e, err := inspect(add, true)
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range e.Writers {
		if w == Other {
			t.Fatalf("adding entries counted as writing a machine folder: %+v", e)
		}
	}
	e, err = inspect(del, true)
	if err != nil {
		t.Fatal(err)
	}
	if !hasOther(e.Writers) {
		t.Fatalf("deleting entries did not count as writing a machine folder: %+v", e)
	}
	if e, err = inspect(add, false); err != nil || !hasOther(e.Writers) {
		t.Fatalf("adding entries to a user root did not count as writing it: %+v %v", e, err)
	}
}
