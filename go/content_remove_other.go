//go:build !windows

package storage

import (
	"context"
	"os"
)

// POSIX unlink permits open readers. Until there is a kernel-backed exclusive
// owner for this operation on POSIX, refuse capability here.
func removeExact(context.Context, *os.File, string, string, RemovalProof, bool, func() error) error {
	return ErrRemovalUnsupported
}

func flushRemovalDirectory(*os.File) error { return ErrRemovalUnsupported }
