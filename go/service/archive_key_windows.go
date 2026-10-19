package service

import (
	"context"
	"errors"
	"os"

	"github.com/openabstractions/abstraction-identity/fileid"
)

func nativeArchiveStagedKey(ctx context.Context, file *os.File) (string, error) {
	held, err := fileid.FromFile(ctx, file)
	if err != nil {
		return "", err
	}
	id := held.FileID()
	closeErr := held.Close()
	if id.Durability() != fileid.Persistent || id.Key() == "" || id.IsDir() {
		return "", errors.Join(fileid.ErrVolatile, closeErr)
	}
	return id.Key(), closeErr
}
