package service

import (
	"context"
	identity "github.com/openabstractions/abstraction-identity"
	storage "github.com/openabstractions/abstraction-storage/go"
	api "github.com/openabstractions/abstraction-storage/go/abstraction/storage/content"
)

// ReadAuthorized composes bounded content access inside a service. authorize
// must decide for the original bound caller; it is called before lookup and on
// each chunk, using the same refusal and file-change checks as the IPC reader.
// Returned bytes remain unverified: the consumer must verify their digest.
func ReadAuthorized(ctx context.Context, store storage.Store, authorize func(context.Context, string) error, digest string, limit int64) ([]byte, string) {
	if store == nil || authorize == nil || limit < 1 {
		return nil, "unavailable"
	}
	r := newRegistry(store, func(ctx context.Context, _ *identity.Peer, d string) error { return authorize(ctx, d) })
	defer r.close()
	c := receiver{registry: r, scope: "composed-call", ctx: ctx}
	opened, err := c.Open(digest)
	if err != nil {
		return nil, "unavailable"
	}
	if opened.Outcome != api.OpenOutcomeOpened {
		return nil, opened.Outcome.String()
	}
	if opened.Resource.Size > limit {
		return nil, "too_large"
	}
	var data []byte
	for {
		page, err := c.Read(opened.Resource.Handle, int64(len(data)), 65536)
		if err != nil {
			return nil, "unavailable"
		}
		if page.Outcome != api.ReadOutcomeData {
			return nil, page.Outcome.String()
		}
		if page.Chunk == nil || int64(len(data))+int64(len(page.Chunk.Data)) > limit {
			return nil, "too_large"
		}
		data = append(data, page.Chunk.Data...)
		if page.Chunk.EOF {
			return data, "read"
		}
		if len(page.Chunk.Data) == 0 {
			return nil, "unavailable"
		}
	}
}
