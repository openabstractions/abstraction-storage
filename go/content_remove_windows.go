package storage

import (
	"context"
	"errors"
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

func removeExact(ctx context.Context, dir *os.File, leaf, rename string, expected RemovalProof, deleteOnClose bool, authorize func() error) (failure error) {
	if err := ctx.Err(); err != nil {
		return err
	}
	name, err := windows.NewNTUnicodeString(leaf)
	if err != nil {
		return err
	}
	attrs := windows.OBJECT_ATTRIBUTES{
		Length:        uint32(unsafe.Sizeof(windows.OBJECT_ATTRIBUTES{})),
		RootDirectory: windows.Handle(dir.Fd()), ObjectName: name, Attributes: windows.OBJ_CASE_INSENSITIVE,
	}
	var held windows.Handle
	var status windows.IO_STATUS_BLOCK
	err = windows.NtCreateFile(&held, windows.DELETE|windows.FILE_READ_DATA|windows.FILE_READ_ATTRIBUTES|windows.SYNCHRONIZE,
		&attrs, &status, nil, 0, 0, windows.FILE_OPEN,
		windows.FILE_NON_DIRECTORY_FILE|windows.FILE_OPEN_REPARSE_POINT|windows.FILE_SYNCHRONOUS_IO_NONALERT, 0, 0)
	if err != nil {
		return removalNativeError(err)
	}
	file := os.NewFile(uintptr(held), leaf)
	defer func() { failure = errors.Join(failure, file.Close()) }()
	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(held, &info); err != nil {
		return err
	}
	if info.NumberOfLinks != 1 || info.FileAttributes&(windows.FILE_ATTRIBUTE_REPARSE_POINT|windows.FILE_ATTRIBUTE_DIRECTORY|windows.FILE_ATTRIBUTE_DEVICE) != 0 {
		return ErrRemovalChanged
	}
	actual, err := removalProof(expected.digest, dir, file)
	if err != nil {
		return err
	}
	actual.persistent = expected.persistent
	if actual != expected {
		return ErrRemovalChanged
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if info.FileAttributes&windows.FILE_ATTRIBUTE_READONLY != 0 {
		return ErrRemovalNeedsAdministrator
	}
	if !deleteOnClose {
		return nil
	}
	if authorize != nil {
		if err := authorize(); err != nil {
			return err
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if rename != "" {
		return removalNativeError(renameRemoval(held, windows.Handle(dir.Fd()), rename))
	}
	// Request delete-on-close only after verifying the exact exclusive handle.
	remove := byte(1)
	return removalNativeError(windows.NtSetInformationFile(held, &status, &remove, 1, windows.FileDispositionInformation))
}

func flushRemovalDirectory(dir *os.File) error {
	name, err := windows.NewNTUnicodeString("")
	if err != nil {
		return err
	}
	attrs := windows.OBJECT_ATTRIBUTES{Length: uint32(unsafe.Sizeof(windows.OBJECT_ATTRIBUTES{})),
		RootDirectory: windows.Handle(dir.Fd()), ObjectName: name}
	var handle windows.Handle
	var status windows.IO_STATUS_BLOCK
	err = windows.NtCreateFile(&handle, windows.FILE_WRITE_DATA|windows.FILE_READ_ATTRIBUTES|windows.SYNCHRONIZE,
		&attrs, &status, nil, 0, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, windows.FILE_OPEN,
		windows.FILE_DIRECTORY_FILE|windows.FILE_OPEN_REPARSE_POINT|windows.FILE_SYNCHRONOUS_IO_NONALERT, 0, 0)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(handle)
	// Match the native ContentStore directory-flush contract on filesystems
	// whose drivers explicitly do not implement directory buffer flushing.
	err = windows.FlushFileBuffers(handle)
	if errors.Is(err, windows.ERROR_INVALID_FUNCTION) || errors.Is(err, windows.ERROR_NOT_SUPPORTED) || errors.Is(err, windows.ERROR_INVALID_PARAMETER) {
		return nil
	}
	return err
}

func renameRemoval(file, directory windows.Handle, name string) error {
	leaf, err := windows.UTF16FromString(name)
	if err != nil {
		return err
	}
	leaf = leaf[:len(leaf)-1]
	type renameInfo struct {
		flags  uint32
		root   windows.Handle
		length uint32
		name   [1]uint16
	}
	var header renameInfo
	offset := unsafe.Offsetof(header.name)
	buffer := make([]byte, int(unsafe.Sizeof(header))+len(leaf)*2)
	info := (*renameInfo)(unsafe.Pointer(&buffer[0]))
	info.root = directory
	info.length = uint32(len(leaf) * 2)
	copy(unsafe.Slice((*uint16)(unsafe.Pointer(&buffer[offset])), len(leaf)), leaf)
	var status windows.IO_STATUS_BLOCK
	return windows.NtSetInformationFile(file, &status, &buffer[0], uint32(len(buffer)), windows.FileRenameInformation)
}

func removalNativeError(err error) error {
	var status windows.NTStatus
	if errors.As(err, &status) {
		if status == windows.STATUS_CANNOT_DELETE {
			return ErrRemovalBusy
		}
		err = status.Errno()
	}
	if errors.Is(err, windows.ERROR_SHARING_VIOLATION) || errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
		return ErrRemovalBusy
	}
	if errors.Is(err, windows.ERROR_ACCESS_DENIED) || errors.Is(err, windows.ERROR_PRIVILEGE_NOT_HELD) {
		return ErrRemovalNeedsAdministrator
	}
	return err
}
