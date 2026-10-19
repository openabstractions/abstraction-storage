//go:build darwin

package layout

import "golang.org/x/sys/unix"

// timeMachineExclusion is the binary property list of the string
// "com.apple.backupd": the value tmutil addexclusion and
// NSURLIsExcludedFromBackupKey write as the sticky exclusion attribute.
var timeMachineExclusion = []byte{
	'b', 'p', 'l', 'i', 's', 't', '0', '0',
	0x5f, 0x10, 0x11, // an ASCII string of 17 bytes
	'c', 'o', 'm', '.', 'a', 'p', 'p', 'l', 'e', '.', 'b', 'a', 'c', 'k', 'u', 'p', 'd',
	0x08,             // the offset table: object 0 at byte 8
	0, 0, 0, 0, 0, 0, // trailer: unused
	1, 1, // offset and reference sizes
	0, 0, 0, 0, 0, 0, 0, 1, // one object
	0, 0, 0, 0, 0, 0, 0, 0, // the top object
	0, 0, 0, 0, 0, 0, 0, 0x1c, // the offset table at byte 28
}

func excludeFromTimeMachine(dir string) error {
	return unix.Setxattr(dir, "com.apple.metadata:com_apple_backup_excludeItem", timeMachineExclusion, 0)
}
