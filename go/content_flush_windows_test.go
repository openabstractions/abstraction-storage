package storage

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestContentCommitOnAdministrativeShare commits into a store on an SMB path,
// where FlushFileBuffers on a directory answers ERROR_INVALID_FUNCTION.
func TestContentCommitOnAdministrativeShare(t *testing.T) {
	local := t.TempDir()
	vol := filepath.VolumeName(local)
	if len(vol) != 2 || vol[1] != ':' {
		t.Skip("temp dir is not on a drive letter")
	}
	share := `\\localhost\` + strings.ToUpper(vol[:1]) + `$` + local[2:]
	if _, err := os.Stat(share); err != nil {
		t.Skipf("administrative share unreachable: %v", err)
	}
	store, err := NewContentStore("share", share)
	if err != nil {
		t.Fatal(err)
	}
	body := []byte("bytes on a share")
	digest := fmt.Sprintf("sha256:%x", sha256.Sum256(body))
	ref, err := store.Place(digest, int64(len(body)))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.Path(ref), body, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := store.Commit(ref); err != nil {
		t.Fatalf("commit on a share: %v", err)
	}
	if _, ok := store.Find(digest); !ok {
		t.Fatal("committed blob not found")
	}
}
