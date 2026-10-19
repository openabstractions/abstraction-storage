//go:build windows

package layout

import (
	"errors"
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Access rights that change or remove what an entry holds. Adding a new
// entry to a folder (FILE_ADD_FILE, FILE_ADD_SUBDIRECTORY) counts for a
// root, which must hold this account's files only, and not for a machine
// folder, since every machine file read is checked itself (JudgeMachine).
const (
	fileWriteData  = 0x2  // FILE_WRITE_DATA, FILE_ADD_FILE
	fileAppendData = 0x4  // FILE_APPEND_DATA, FILE_ADD_SUBDIRECTORY
	fileDeleteKid  = 0x40 // FILE_DELETE_CHILD
	changeMask     = windows.DELETE | windows.WRITE_DAC | windows.WRITE_OWNER | windows.GENERIC_WRITE | windows.GENERIC_ALL
	readMask       = windows.FILE_READ_DATA | windows.FILE_READ_EA | windows.FILE_EXECUTE | windows.FILE_READ_ATTRIBUTES |
		windows.READ_CONTROL | windows.GENERIC_READ | windows.GENERIC_EXECUTE
	inheritOnly = 0x8 // INHERIT_ONLY_ACE
)

// trustedInstaller is NT SERVICE\TrustedInstaller.
const trustedInstaller = "S-1-5-80-956008885-3418522649-1831038044-1853292631-2271478464"

// readEntry reads one entry's owner and DACL. A reparse point is reported as
// a link for a root (STO-L9); a machine entry is judged by what a reader
// opens (STO-L4).
func readEntry(p string, machine bool) (Entry, error) {
	e := Entry{Path: p}
	name, err := windows.UTF16PtrFromString(p)
	if err != nil {
		return e, err
	}
	attributes, err := windows.GetFileAttributes(name)
	if errors.Is(err, windows.ERROR_FILE_NOT_FOUND) || errors.Is(err, windows.ERROR_PATH_NOT_FOUND) {
		return e, nil
	}
	if err != nil {
		return e, err
	}
	e.Exists = true
	e.Dir = attributes&windows.FILE_ATTRIBUTE_DIRECTORY != 0
	e.Link = attributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 && !machine
	sd, err := windows.GetNamedSecurityInfo(p, windows.SE_FILE_OBJECT,
		windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return e, err
	}
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return e, err
	}
	owner, _, err := sd.Owner()
	if err != nil {
		return e, err
	}
	if owner == nil {
		return e, errors.New("it has no owner")
	}
	e.Owner, e.Mine = holderOf(owner, user.User.Sid), owner.Equals(user.User.Sid)
	e.OwnerName = accountName(owner)
	write := uint32(changeMask)
	if e.Dir {
		write |= fileDeleteKid
		if !machine {
			write |= fileWriteData | fileAppendData
		}
	} else {
		write |= fileWriteData | fileAppendData
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		return e, err
	}
	if dacl == nil {
		// A missing DACL admits everyone.
		e.Writers = []Holder{Other}
		return e, nil
	}
	for i := uint32(0); i < uint32(dacl.AceCount); i++ {
		var ace *windows.ACCESS_ALLOWED_ACE
		if err := windows.GetAce(dacl, i, &ace); err != nil {
			return e, err
		}
		if ace.Header.AceFlags&inheritOnly != 0 {
			continue
		}
		switch ace.Header.AceType {
		case windows.ACCESS_ALLOWED_ACE_TYPE:
		case windows.ACCESS_DENIED_ACE_TYPE:
			// A deny entry only narrows access.
			continue
		default:
			return e, fmt.Errorf("its DACL holds an entry of type %d", ace.Header.AceType)
		}
		who := holderOf((*windows.SID)(unsafe.Pointer(&ace.SidStart)), user.User.Sid)
		switch {
		case uint32(ace.Mask)&write != 0:
			e.Writers = appendOnce(e.Writers, who)
		case uint32(ace.Mask)&readMask != 0:
			e.Readers = appendOnce(e.Readers, who)
		}
	}
	return e, nil
}

func holderOf(sid, user *windows.SID) Holder {
	switch s := sid.String(); {
	case s == "S-1-5-18", s == "S-1-5-32-544", s == trustedInstaller:
		return Admin
	case sid.Equals(user):
		return Self
	}
	return Other
}

func accountName(sid *windows.SID) string {
	s := sid.String()
	if account, domain, _, err := sid.LookupAccount(""); err == nil {
		return domain + `\` + account + " (" + s + ")"
	}
	return s
}

func appendOnce(list []Holder, h Holder) []Holder {
	for _, x := range list {
		if x == h {
			return list
		}
	}
	return append(list, h)
}
