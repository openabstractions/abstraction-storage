// Package client reads explicitly authorized content through one service binding.
package client

import (
	"context"
	"errors"
	"github.com/openabstractions/abstraction-identity/listen"
	api "github.com/openabstractions/abstraction-storage/go/abstraction/storage/content"
	"strings"
	"time"
)

type Resource = api.Resource
type Client struct{ transport listen.FrameClient }

func New(endpoint string) *Client {
	return NewWithTransport(listen.FrameClient{Endpoint: endpoint})
}

// NewWithTransport retains the caller's endpoint, server trust and waiting limits.
func NewWithTransport(transport listen.FrameClient) *Client {
	return &Client{transport.WithDefaults(5*time.Second, 1<<20)}
}
func validDigest(d string) bool {
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
func validResource(r Resource) bool {
	return r.Handle != "" && len(r.Handle) <= 128 && r.Size >= 0 && r.Verification == "unverified" && validDigest(r.Digest)
}
func (c *Client) Open(ctx context.Context, digest string) (api.OpenResult, error) {
	if !validDigest(digest) {
		return api.OpenResult{}, errors.New("storage: canonical SHA256 digest required")
	}
	if e := ctx.Err(); e != nil {
		return api.OpenResult{}, e
	}
	r, e := api.NewContentReaderClient(c.transport.WithContext(ctx)).Open(digest)
	if e != nil {
		return api.OpenResult{}, e
	}
	if (r.Outcome == "opened") != (r.Resource != nil) || r.Resource != nil && (!validResource(*r.Resource) || r.Resource.Digest != digest) {
		return api.OpenResult{}, errors.New("storage: inconsistent open result")
	}
	return r, nil
}
func (c *Client) Read(ctx context.Context, resource Resource, offset, maxBytes int64) (api.ReadResult, error) {
	if !validResource(resource) || offset < 0 || offset > resource.Size || maxBytes < 1 || maxBytes > 65536 {
		return api.ReadResult{}, errors.New("storage: invalid read bounds")
	}
	if e := ctx.Err(); e != nil {
		return api.ReadResult{}, e
	}
	r, e := api.NewContentReaderClient(c.transport.WithContext(ctx)).Read(resource.Handle, offset, maxBytes)
	if e != nil {
		return api.ReadResult{}, e
	}
	if e = validateRead(r, resource, offset, maxBytes); e != nil {
		return api.ReadResult{}, e
	}
	return r, nil
}
func validateRead(r api.ReadResult, resource Resource, offset, maxBytes int64) error {
	if (r.Outcome == "data") != (r.Chunk != nil) {
		return errors.New("storage: inconsistent read outcome")
	}
	if r.Chunk == nil {
		return nil
	}
	c := r.Chunk
	n := int64(len(c.Data))
	if c.Offset != offset || c.Total != resource.Size || n > maxBytes || n > resource.Size-offset || c.Eof != (offset+n == resource.Size) || (n == 0 && !c.Eof) {
		return errors.New("storage: inconsistent content chunk")
	}
	return nil
}
func (c *Client) Close(ctx context.Context, resource Resource) (api.CloseResult, error) {
	if !validResource(resource) {
		return api.CloseResult{}, errors.New("storage: invalid resource")
	}
	if e := ctx.Err(); e != nil {
		return api.CloseResult{}, e
	}
	return api.NewContentReaderClient(c.transport.WithContext(ctx)).Close(resource.Handle)
}
