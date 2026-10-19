//go:build !windows

package service

import (
	"context"
	"os"

	storage "github.com/openabstractions/abstraction-storage/go"
)

func nativeArchiveStagedKey(context.Context, *os.File) (string, error) {
	return "", storage.ErrRemovalUnsupported
}
