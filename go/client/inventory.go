package client

import (
	"context"
	"errors"
	"time"

	"github.com/openabstractions/abstraction-identity/listen"
	api "github.com/openabstractions/abstraction-storage/go/abstraction/storage/content"
)

type InventoryPage = api.InventoryPage
type ManifestHolders = api.ManifestHolders
type ObjectHolders = api.ObjectHolders
type Store = api.Store

// Inventory reads what the machine holds and who depends on it, composed by
// the service from its own tables and every designated source. Every call is
// subject to the service's inventory.read gate and its per-digest read policy.
type Inventory struct{ transport listen.FrameClient }

func NewInventory(endpoint string) *Inventory {
	return NewInventoryWithTransport(listen.FrameClient{Endpoint: endpoint})
}

// NewInventoryWithTransport retains the caller's endpoint, server trust and
// waiting limits.
func NewInventoryWithTransport(transport listen.FrameClient) *Inventory {
	return &Inventory{transport.WithDefaults(30*time.Second, 1<<20)}
}

func (c *Inventory) call(ctx context.Context) *api.InventoryClient {
	return api.NewInventoryClient(c.transport.WithContext(ctx))
}

// validInventoryPage refuses a page whose shape the contract rules out.
func validInventoryPage(p api.InventoryPage, continuation string, limit int64) error {
	if p.Outcome != api.InventoryOutcomePage {
		if len(p.Stores) != 0 || len(p.Manifests) != 0 || len(p.Objects) != 0 || len(p.Dangling) != 0 ||
			p.Continuation != "" || p.Cursor != "" || p.Complete {
			return errors.New("storage: inconsistent inventory refusal")
		}
		return nil
	}
	if int64(len(p.Manifests)+len(p.Objects)) > limit || len(p.Continuation) > 256 {
		return errors.New("storage: inconsistent inventory page")
	}
	if p.Complete == (p.Continuation != "") {
		return errors.New("storage: inconsistent inventory continuation")
	}
	if continuation != "" && len(p.Stores) != 0 {
		return errors.New("storage: stores repeated after the first page")
	}
	return nil
}

// List pages one frozen composition. An empty continuation freezes a new one.
func (c *Inventory) List(ctx context.Context, continuation string, limit int64) (api.InventoryPage, error) {
	if len(continuation) > 256 || limit < 1 || limit > 256 {
		return api.InventoryPage{}, errors.New("storage: invalid inventory request")
	}
	page, err := c.call(ctx).List(continuation, limit)
	if err != nil {
		return api.InventoryPage{}, err
	}
	if err := validInventoryPage(page, continuation, limit); err != nil {
		return api.InventoryPage{}, err
	}
	return page, nil
}

// Holders re-observes every hold on one manifest id or digest now.
func (c *Inventory) Holders(ctx context.Context, target string) (api.InventoryPage, error) {
	if target == "" || len(target) > 1024 {
		return api.InventoryPage{}, errors.New("storage: invalid inventory target")
	}
	page, err := c.call(ctx).Holders(target)
	if err != nil {
		return api.InventoryPage{}, err
	}
	return page, nil
}

// Unheld lists what no holder depends on, oldest first.
func (c *Inventory) Unheld(ctx context.Context, continuation string, limit int64) (api.InventoryPage, error) {
	if len(continuation) > 256 || limit < 1 || limit > 256 {
		return api.InventoryPage{}, errors.New("storage: invalid inventory request")
	}
	page, err := c.call(ctx).Unheld(continuation, limit)
	if err != nil {
		return api.InventoryPage{}, err
	}
	return page, nil
}

// Find lists what carries one canonical digest, and the references that want it.
func (c *Inventory) Find(ctx context.Context, digest string) (api.InventoryPage, error) {
	if !validDigest(digest) {
		return api.InventoryPage{}, errors.New("storage: invalid digest")
	}
	page, err := c.call(ctx).Find(digest)
	if err != nil {
		return api.InventoryPage{}, err
	}
	return page, nil
}
