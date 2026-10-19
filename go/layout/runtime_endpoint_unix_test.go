//go:build unix

package layout

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

func TestRuntimeEndpointRootPreservesControlNamespaceAndNoCreation(t *testing.T) {
	base := t.TempDir()
	if err := os.Chmod(base, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_RUNTIME_DIR", base)
	t.Setenv("HOME", "relative-profile-must-not-be-resolved")
	got, err := RuntimeEndpointRoot()
	if want := filepath.Join(base, Name); err != nil || got != want {
		t.Fatalf("root %q %v, want %q", got, err, want)
	}
	if _, err := os.Lstat(got); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("lookup created namespace: %v", err)
	}
	t.Setenv("XDG_RUNTIME_DIR", "")
	t.Setenv("TMPDIR", base)
	got, err = RuntimeEndpointRoot()
	if want := filepath.Join(base, Name+"-"+strconv.Itoa(os.Geteuid())); err != nil || got != want {
		t.Fatalf("fallback %q %v, want %q", got, err, want)
	}
	if _, err := os.Lstat(got); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("lookup created fallback: %v", err)
	}
}

func TestRuntimeEndpointRootRefusesInsteadOfSubstituting(t *testing.T) {
	base := t.TempDir()
	if err := os.Chmod(base, 0o700); err != nil {
		t.Fatal(err)
	}
	uid := os.Geteuid()
	for _, bad := range []string{"relative", filepath.Join(base, "missing")} {
		if got, err := runtimeEndpointRoot(uid, uid, bad, base); err == nil || got != "" {
			t.Fatalf("bad XDG accepted/fell back: %q %v", got, err)
		}
	}
	if got, err := runtimeEndpointRoot(uid+1, uid, base, base); err == nil || got != "" {
		t.Fatalf("mixed identities accepted: %q %v", got, err)
	}
	if got, err := runtimeEndpointRoot(uid, uid, "", "relative"); err == nil || got != "" {
		t.Fatalf("relative temporary root accepted: %q %v", got, err)
	}
	if err := os.Chmod(base, 0o755); err != nil {
		t.Fatal(err)
	}
	if got, err := runtimeEndpointRoot(uid, uid, base, t.TempDir()); err == nil || got != "" {
		t.Fatalf("insecure XDG accepted/fell back: %q %v", got, err)
	}
	if err := os.Chmod(base, 0o700); err != nil {
		t.Fatal(err)
	}
	namespace := filepath.Join(base, Name)
	if err := os.Mkdir(namespace, 0o755); err != nil {
		t.Fatal(err)
	}
	if got, err := runtimeEndpointRoot(uid, uid, base, ""); err == nil || got != "" {
		t.Fatalf("insecure namespace accepted: %q %v", got, err)
	}
	if err := os.Remove(namespace); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(t.TempDir(), namespace); err != nil {
		t.Fatal(err)
	}
	if got, err := runtimeEndpointRoot(uid, uid, base, ""); err == nil || got != "" {
		t.Fatalf("redirected namespace accepted: %q %v", got, err)
	}
}
