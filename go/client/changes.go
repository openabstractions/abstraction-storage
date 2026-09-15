package client

import (
	"context"
	"errors"
	"github.com/openabstractions/abstraction-identity/listen"
	api "github.com/openabstractions/abstraction-storage/go/abstraction/storage/content"
	"time"
)

type Change = api.Change
type ChangePage = api.ChangePage
type ListedObject = api.ListedObject

// Changes observes objects a store gains or loses through one binding. Every
// call is subject to the service's observe and per-object read policies.
type Changes struct{ transport listen.FrameClient }

func NewChanges(endpoint string) *Changes {
	return NewChangesWithTransport(listen.FrameClient{Endpoint: endpoint})
}

// NewChangesWithTransport retains the caller's endpoint, server trust and waiting limits.
func NewChangesWithTransport(transport listen.FrameClient) *Changes {
	return &Changes{transport.WithDefaults(5*time.Second, 1<<20)}
}

func (c *Changes) call(ctx context.Context, waitMS int64) *api.ContentChangesClient {
	t := c.transport
	if extra := time.Duration(waitMS) * time.Millisecond; extra > 0 {
		t.Timeout += extra
	}
	return api.NewContentChangesClient(t.WithContext(ctx))
}

func validChangePage(p api.ChangePage, cursor string, max int64) error {
	if p.Outcome != api.ChangePageOutcomePage {
		if len(p.Changes) != 0 || p.Next != cursor || p.AtEnd {
			return errors.New("storage: inconsistent change refusal")
		}
		return nil
	}
	if int64(len(p.Changes)) > max || p.Next == "" || len(p.Next) > 256 {
		return errors.New("storage: inconsistent change page")
	}
	var last int64
	for _, ch := range p.Changes {
		if ch.Sequence <= last || !validDigest(ch.Digest) || ch.Size < 0 || (ch.Kind != api.ChangeKindAdded && ch.Kind != api.ChangeKindRemoved) {
			return errors.New("storage: inconsistent change entry")
		}
		last = ch.Sequence
	}
	return nil
}

// Observe returns changes after cursor. An empty cursor starts at the current
// end. A gap requires rebuilding state from Snapshot. There is no retry.
func (c *Changes) Observe(ctx context.Context, cursor string, maxChanges, waitMS int64) (api.ChangePage, error) {
	if len(cursor) > 256 || maxChanges < 1 || maxChanges > 256 || waitMS < 0 || waitMS > 30000 {
		return api.ChangePage{}, errors.New("storage: invalid change request")
	}
	if e := ctx.Err(); e != nil {
		return api.ChangePage{}, e
	}
	p, e := c.call(ctx, waitMS).Observe(cursor, maxChanges, waitMS)
	if e != nil {
		return api.ChangePage{}, e
	}
	if e = validChangePage(p, cursor, maxChanges); e != nil {
		return api.ChangePage{}, e
	}
	return p, nil
}

func validListingPage(p api.ListingPage, limit int64) error {
	if p.Outcome != api.ListingOutcomePage {
		if len(p.Objects) != 0 || p.Continuation != "" || p.Cursor != "" || p.Complete {
			return errors.New("storage: inconsistent listing refusal")
		}
		return nil
	}
	if int64(len(p.Objects)) > limit || p.Cursor == "" || p.Complete != (p.Continuation == "") || len(p.Continuation) > 256 {
		return errors.New("storage: inconsistent listing page")
	}
	previous := ""
	for _, o := range p.Objects {
		if !validDigest(o.Digest) || o.Size < 0 || o.Digest <= previous {
			return errors.New("storage: inconsistent listed object")
		}
		previous = o.Digest
	}
	return nil
}

// List returns one page of a frozen snapshot. An empty continuation starts a new snapshot.
func (c *Changes) List(ctx context.Context, continuation string, limit int64) (api.ListingPage, error) {
	if len(continuation) > 256 || limit < 1 || limit > 256 {
		return api.ListingPage{}, errors.New("storage: invalid listing request")
	}
	if e := ctx.Err(); e != nil {
		return api.ListingPage{}, e
	}
	p, e := c.call(ctx, 0).List(continuation, limit)
	if e != nil {
		return api.ListingPage{}, e
	}
	if e = validListingPage(p, limit); e != nil {
		return api.ListingPage{}, e
	}
	return p, nil
}

// Snapshot reads every page of one snapshot and returns its objects and the
// change cursor at which it was taken. Observing from that cursor reports every
// later change. A refusal or gap is returned as *OutcomeError.
func (c *Changes) Snapshot(ctx context.Context, limit int64) ([]ListedObject, string, error) {
	var objects []ListedObject
	continuation, cursor := "", ""
	for {
		p, err := c.List(ctx, continuation, limit)
		if err != nil {
			return nil, "", err
		}
		if p.Outcome != api.ListingOutcomePage {
			return nil, "", &OutcomeError{"list", p.Outcome}
		}
		if cursor != "" && p.Cursor != cursor {
			return nil, "", errors.New("storage: snapshot cursor changed between pages")
		}
		cursor = p.Cursor
		objects = append(objects, p.Objects...)
		if p.Complete {
			return objects, cursor, nil
		}
		continuation = p.Continuation
	}
}
