//go:build darwin

package layout

import (
	"bytes"
	"os/exec"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// [STO-L7] On macOS a no-backup folder carries the sticky Time Machine
// exclusion: the attribute holds the binary property list tmutil writes, and
// tmutil, where the system has it, reports the folder excluded.
func TestMarkNoBackupSetsTheTimeMachineExclusion(t *testing.T) {
	dir := t.TempDir()
	if err := MarkNoBackup(dir); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 256)
	n, err := unix.Getxattr(dir, "com.apple.metadata:com_apple_backup_excludeItem", buf)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(buf[:n], timeMachineExclusion) {
		t.Fatalf("the exclusion attribute holds %x, want %x", buf[:n], timeMachineExclusion)
	}
	tmutil, err := exec.LookPath("tmutil")
	if err != nil {
		t.Logf("no tmutil here, so Time Machine's reading of the attribute is not checked: %v", err)
		return
	}
	out, err := exec.Command(tmutil, "isexcluded", dir).CombinedOutput()
	if err != nil {
		t.Fatalf("tmutil isexcluded: %v: %s", err, out)
	}
	if !strings.Contains(string(out), "[Excluded]") {
		t.Fatalf("tmutil does not report the folder excluded: %s", out)
	}
}
