package digest

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The validators CQ-12 removed, kept verbatim as the differential oracle.

func oldStorageClient(d string) bool { // storage go/client/client.go validDigest
	if len(d) != 71 || !strings.HasPrefix(d, "sha256:") {
		return false
	}
	for _, c := range d[7:] {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

func oldStorageService(d string) bool { // storage go/service/resources.go validDigest; storage.ValidDigest
	if len(d) != 71 || d[:7] != "sha256:" {
		return false
	}
	for _, c := range d[7:] {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

func oldMediationContent(digest string) bool { // inference go/mediationcontent/transfer.go validDigest
	if len(digest) != 71 || !strings.HasPrefix(digest, "sha256:") {
		return false
	}
	for _, c := range digest[7:] {
		if c < '0' || c > '9' && c < 'a' || c > 'f' {
			return false
		}
	}
	return true
}

func oldInferenceValidate(digest string) bool { // inference go/validate.go canonicalSHA256
	return oldStorageClient(digest)
}

func oldDownloadBareHex(d string) string { // download go/spec.go bareHex
	s := strings.ToLower(strings.TrimSpace(d))
	s = strings.TrimPrefix(s, "sha256:")
	s = strings.TrimPrefix(s, "sha256-")
	if len(s) != 64 {
		return ""
	}
	for _, c := range s {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return ""
		}
	}
	return s
}

func corpus() []string {
	hexes := []string{
		strings.Repeat("a", 64),
		strings.Repeat("0123456789abcdef", 4),
		strings.Repeat("F", 64),
		strings.Repeat("0123456789ABCDEF", 4),
		strings.Repeat("a", 63),
		strings.Repeat("a", 65),
		strings.Repeat("g", 64),
		strings.Repeat("a", 62) + "é",
		strings.Repeat("a", 63) + "/",
		"../../x" + strings.Repeat("a", 57),
		"",
	}
	labels := []string{"", "sha256:", "SHA256:", "sha256-", "sha512:", "sha256::", " sha256:"}
	wraps := []func(string) string{
		func(s string) string { return s },
		func(s string) string { return " " + s },
		func(s string) string { return s + "\n" },
		func(s string) string { return "\t" + s + " " },
		strings.ToUpper,
	}
	var out []string
	for _, h := range hexes {
		for _, l := range labels {
			for _, w := range wraps {
				out = append(out, w(l+h))
			}
		}
	}
	return append(out, "sha256:../../x", "sha256:", "sha256-", "x")
}

func TestValidEqualsRemovedValidators(t *testing.T) {
	oracles := map[string]func(string) bool{
		"storage client":     oldStorageClient,
		"storage service":    oldStorageService,
		"mediationcontent":   oldMediationContent,
		"inference validate": oldInferenceValidate,
	}
	accepted := 0
	for _, d := range corpus() {
		got := Valid(d)
		if got {
			accepted++
		}
		for name, old := range oracles {
			if old(d) != got {
				t.Errorf("Valid(%q) = %v, %s = %v", d, got, name, old(d))
			}
		}
	}
	if accepted == 0 {
		t.Fatal("corpus has no valid digest")
	}
}

func TestNormalizeEqualsDownloadBareHex(t *testing.T) {
	for _, d := range corpus() {
		if got, want := Normalize(d), oldDownloadBareHex(d); got != want {
			t.Errorf("Normalize(%q) = %q, bareHex = %q", d, got, want)
		}
	}
}

func TestRefusesServiceBoundaryForms(t *testing.T) {
	h := strings.Repeat("ab", 32)
	for _, d := range []string{"sha256:../../x", "sha256-" + h, h, "sha256:" + strings.ToUpper(h), " sha256:" + h} {
		if Valid(d) {
			t.Errorf("Valid(%q) accepted", d)
		}
	}
	if !Valid("sha256:" + h) {
		t.Error("strict form refused")
	}
}

func TestFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f")
	data := []byte("content")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	var last int64
	n, d, err := File(context.Background(), path, func(done, total int64) { last = done })
	if err != nil || n != int64(len(data)) || d != "sha256:"+hex.EncodeToString(sum[:]) || last != n || !Valid(d) {
		t.Fatalf("File = %d %q %v (progress %d)", n, d, err, last)
	}
	stopped, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := File(stopped, path, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled File = %v", err)
	}
}
