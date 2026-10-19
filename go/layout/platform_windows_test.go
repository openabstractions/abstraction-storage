package layout

import (
	"errors"
	"testing"

	"github.com/openabstractions/abstraction-identity/go/profile"
	"golang.org/x/sys/windows"
)

func TestProfileViewUsesSharedIdentityOwner(t *testing.T) {
	want, err := profile.Current()
	if err != nil {
		t.Fatal(err)
	}
	got, err := profileView()
	if err != nil {
		t.Fatal(err)
	}
	expected := ""
	if want.Virtualized {
		expected = want.Family
	}
	if got != expected {
		t.Fatalf("profile owner differs: %q, want %q", got, expected)
	}
}

func TestNativeKnownFolderLookupKeepsLegacyRoamingSeparate(t *testing.T) {
	platform := Current()
	platform.Profile = func() (string, error) { t.Fatal("native folder lookup probed the profile view"); return "", nil }
	platform.Temp = func() (string, error) { t.Fatal("native folder lookup resolved a temporary root"); return "", nil }
	local, roaming, err := WindowsAccountFolders(platform)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name string
		id   *windows.KNOWNFOLDERID
	}{{"LocalAppData", windows.FOLDERID_LocalAppData}, {"RoamingAppData", windows.FOLDERID_RoamingAppData}} {
		got := local
		if test.name == "RoamingAppData" {
			got = roaming
		}
		want, err := windows.KnownFolderPath(test.id, windows.KF_FLAG_DEFAULT)
		if err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Fatalf("%s resolved %q, want its native account folder %q", test.name, got, want)
		}
	}
}

func TestWindowsAccountFoldersRefusesInvalidMetadata(t *testing.T) {
	for _, test := range []struct {
		name, goos string
		known      func(string) (string, error)
	}{
		{"unsupported", "linux", func(string) (string, error) { t.Fatal("unsupported platform read folders"); return "", nil }},
		{"missing callback", "windows", nil},
		{"callback error", "windows", func(string) (string, error) { return "", errors.New("unavailable") }},
		{"relative local", "windows", func(string) (string, error) { return "relative", nil }},
		{"relative roaming", "windows", func(name string) (string, error) {
			if name == "LocalAppData" {
				return `C:\local`, nil
			}
			return "relative", nil
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			local, roaming, err := WindowsAccountFolders(Platform{GOOS: test.goos, Known: test.known})
			if err == nil || local != "" || roaming != "" {
				t.Fatalf("invalid metadata yielded %q, %q, %v", local, roaming, err)
			}
		})
	}
}
