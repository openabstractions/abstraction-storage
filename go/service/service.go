// Package service adapts explicitly configured storage providers to shared IPC.
package service

import (
	"context"
	"encoding/json"
	"errors"
	casapi "github.com/openabstractions/abstraction-cas/go/api"
	identity "github.com/openabstractions/abstraction-identity"
	"github.com/openabstractions/abstraction-identity/listen"
	storage "github.com/openabstractions/abstraction-storage/go"
	api "github.com/openabstractions/abstraction-storage/go/abstraction/storage/content"
	"os/user"
	"path/filepath"
	"strconv"
	"sync"
	"time"
)

const MaxFrameBytes = 1 << 20

type Host struct {
	*Component
	listener  listen.Listener
	once      sync.Once
	workers   sync.WaitGroup
	slots     chan struct{}
	OnStopped func()
}

// Component owns storage policy, handles, uploads, and optional profiles for
// one provider lifetime. A caller may serve its dispatchers on an existing
// authenticated framed endpoint.
type Component struct {
	registry  *registry
	writers   *writers
	changes   *changeJournal
	inventory *inventory
	interval  time.Duration
	owner     string
	ctx       context.Context
	cancel    context.CancelFunc
	once      sync.Once
	lifecycle sync.Mutex
	serving   bool
	stopStart func() bool
	workers   sync.WaitGroup
	OnError   func(error)
}

// Listen owns no Store configuration. Policy is mandatory and never replaced by
// a same-account blanket grant. Configure callbacks before Serve.
func Listen(endpoint string, store storage.Store, policy Policy) (*Host, error) {
	component, e := NewComponent(store, policy)
	if e != nil {
		return nil, e
	}
	l, e := listen.ListenFramed(endpoint, listen.Program)
	if e != nil {
		component.Close()
		return nil, e
	}
	return &Host{Component: component, listener: listen.Sessions(l, listen.SessionOptions{MaxSessions: 32}), slots: make(chan struct{}, 32)}, nil
}

// NewComponent requires an explicit store and read policy. The principal is
// captured from the service account and checked against every framed peer.
func NewComponent(store storage.Store, policy Policy) (*Component, error) {
	if store == nil || policy == nil {
		return nil, errors.New("storage: explicit store and content policy required")
	}
	owner, e := user.Current()
	if e != nil {
		return nil, e
	}
	if owner.Uid == "" {
		return nil, errors.New("storage: service principal unavailable")
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &Component{registry: newRegistry(store, policy), owner: owner.Uid, ctx: ctx, cancel: cancel}, nil
}
func (h *Host) Close() error {
	var e error
	h.once.Do(func() {
		e = h.listener.Close()
		h.Component.Close()
	})
	return e
}

// Close stops admission and closes retained handles and staging files.
func (c *Component) Close() error {
	c.once.Do(func() {
		c.cancel()
		c.registry.close()
		c.lifecycle.Lock()
		writers, changes, composed := c.writers, c.changes, c.inventory
		stopStart := c.stopStart
		c.lifecycle.Unlock()
		if stopStart != nil {
			stopStart()
		}
		if writers != nil {
			writers.close()
		}
		if changes != nil {
			changes.close()
		}
		if composed != nil {
			composed.close()
		}
	})
	c.workers.Wait()
	return nil
}

// EnableChanges adds abstraction.storage/content-changes@1 to this endpoint.
// observe authorizes each Observe and List call on ChangesResource; the read
// policy filters every change and listed object per digest. The present objects
// of a Lister provider are recorded without being journaled, and the provider is
// polled every interval for external additions and deletions. capacity bounds
// the journal. Configure it before Serve.
func (h *Component) EnableChanges(observe Policy, interval time.Duration, capacity int) error {
	h.lifecycle.Lock()
	defer h.lifecycle.Unlock()
	if h.serving || h.ctx.Err() != nil {
		return errors.New("storage: configure change observation before Serve")
	}
	if observe == nil || capacity < 1 || interval <= 0 {
		return errChangesUnsupported
	}
	journal, err := newChangeJournal(h.registry.store, h.registry.policy, observe, capacity)
	if err != nil {
		return err
	}
	h.changes, h.interval = journal, interval
	return nil
}

// ChangesAvailable reports whether change observation is configured and serving.
func (h *Component) ChangesAvailable() bool {
	h.lifecycle.Lock()
	defer h.lifecycle.Unlock()
	return h.changes != nil && h.ctx.Err() == nil
}

// EnableWriter adds abstraction.storage/content-writer@1 to this endpoint. The
// configured store must supply Local and Writable. The write policy is separate
// from the read policy; limit is the maximum declared object size in bytes.
// records is service-owned atomic state at the absolute recordPath; this host
// must be its only writer. Existing identities are restored and staging left by
// their unfinished uploads is removed before this returns.
func (h *Component) EnableWriter(policy Policy, limit int64, records casapi.Store, recordPath string) error {
	h.lifecycle.Lock()
	defer h.lifecycle.Unlock()
	if h.serving || h.ctx.Err() != nil {
		return errors.New("storage: configure writer before Serve")
	}
	if policy == nil || limit < 1 || records == nil || !filepath.IsAbs(recordPath) {
		return errors.New("storage: explicit write policy, positive size limit and service-owned record store required")
	}
	store, ok := h.registry.store.(WritableStore)
	if !ok {
		return errors.New("storage: provider does not support bounded writes")
	}
	w, err := newWriters(store, policy, limit, records, recordPath, time.Now)
	if err != nil {
		return err
	}
	w.report = func(err error) {
		if h.OnError != nil {
			h.OnError(err)
		}
	}
	w.failed(w.unreported)
	w.unreported = nil
	h.writers = w
	return nil
}

// WriterAvailable reports whether the writer profile is configured and serving.
func (h *Component) WriterAvailable() bool {
	h.lifecycle.Lock()
	defer h.lifecycle.Unlock()
	return h.writers != nil && h.ctx.Err() == nil
}
func (h *Host) Serve(ctx context.Context) error {
	if err := h.Component.Start(ctx); err != nil {
		return err
	}
	stop := context.AfterFunc(ctx, func() { h.Close() })
	defer stop()
	defer h.workers.Wait()
	defer h.Close()
	defer func() {
		if h.OnStopped != nil {
			h.OnStopped()
		}
	}()
	for {
		conn, e := h.listener.Accept()
		if e != nil {
			if h.ctx.Err() != nil || ctx.Err() != nil {
				return nil
			}
			return e
		}
		select {
		case h.slots <- struct{}{}:
		default:
			conn.Close()
			continue
		}
		h.workers.Add(1)
		go func() {
			defer h.workers.Done()
			defer func() { <-h.slots }()
			defer conn.Close()
			callCtx, cancel := context.WithTimeout(h.ctx, 35*time.Second)
			defer cancel()
			call, e := listen.ReceiveFramed(callCtx, conn, listen.Program, MaxFrameBytes)
			if call != nil {
				defer call.Close()
			}
			if e == nil {
				var reply []byte
				reply, e = api.ServeEndpoint(call.Frame, "openabstractions", "", h.Component.Services(call, callCtx)...)
				if e == nil {
					e = call.Reply(reply)
				}
			}
			if e != nil && h.OnError != nil && h.ctx.Err() == nil {
				h.OnError(errors.New("storage: framed request failed"))
			}
		}()
	}
}

// Start begins expiration and change polling for this component lifetime.
// Close joins the background workers. A cancelled parent closes the component.
func (h *Component) Start(ctx context.Context) error {
	h.lifecycle.Lock()
	if h.serving {
		h.lifecycle.Unlock()
		return errors.New("storage: component already started")
	}
	if h.ctx.Err() != nil || ctx == nil || ctx.Err() != nil {
		h.lifecycle.Unlock()
		return errors.New("storage: component closed or context cancelled")
	}
	h.serving = true
	writers, changes, interval, composed := h.writers, h.changes, h.interval, h.inventory
	h.workers.Add(1)
	if changes != nil {
		h.workers.Add(1)
	}
	h.stopStart = context.AfterFunc(ctx, func() { h.Close() })
	h.lifecycle.Unlock()
	if writers != nil && changes != nil {
		writers.mu.Lock()
		writers.onCommitted = changes.committed
		writers.mu.Unlock()
	}
	if changes != nil {
		go func() {
			defer h.workers.Done()
			ticker := time.NewTicker(interval)
			defer ticker.Stop()
			for {
				select {
				case <-h.ctx.Done():
					return
				case <-ticker.C:
					changes.poll()
					changes.sweep()
				}
			}
		}()
	}
	go func() {
		defer h.workers.Done()
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-h.ctx.Done():
				return
			case <-ticker.C:
				h.registry.sweep()
				if writers != nil {
					writers.sweep()
				}
				if composed != nil {
					composed.sweep()
				}
			}
		}
	}()
	return nil
}

// Services derives the subject from this exact authenticated call. A failed
// peer proof leaves an empty scope, which every receiver refuses. The composing
// host must cancel callCtx after replying so per-call cancellation hooks are
// released; its call context should also carry a bounded deadline.
func (h *Component) Services(call *listen.FramedCall, callCtx context.Context) []api.ServedService {
	if callCtx == nil {
		callCtx = context.Background()
	}
	// The hosting listener owns the per-call deadline; the component's own
	// shutdown must also cancel in-flight policy decisions on a shared endpoint.
	active, cancel := context.WithCancel(callCtx)
	stop := context.AfterFunc(h.ctx, cancel)
	context.AfterFunc(active, func() { stop() })
	callCtx = active
	var peer *identity.Peer
	if call != nil {
		proven, err := call.Peer()
		if err == nil {
			peer = proven
		}
	}
	scope := callerScope(peer, h.owner)
	h.lifecycle.Lock()
	writers, changes, composed := h.writers, h.changes, h.inventory
	h.lifecycle.Unlock()
	services := []api.ServedService{&api.ContentReaderDispatcher{Handler: receiver{registry: h.registry, scope: scope, peer: peer, ctx: callCtx}}}
	if writers != nil {
		services = append(services, &api.ContentWriterDispatcher{Handler: writeReceiver{writers: writers, scope: scope, peer: peer, ctx: callCtx}})
	}
	if changes != nil {
		var wait context.Context
		if call != nil {
			wait = call.WaitContext()
		}
		services = append(services, &api.ContentChangesDispatcher{Handler: changesReceiver{journal: changes, scope: scope, peer: peer, ctx: callCtx, wait: wait}})
	}
	if composed != nil {
		services = append(services, &api.InventoryDispatcher{Handler: inventoryReceiver{inventory: composed, scope: scope, peer: peer, ctx: callCtx}})
	}
	return services
}
func callerScope(peer *identity.Peer, owner string) string {
	if peer == nil {
		return ""
	}
	u, e := peer.User.AtLeast(listen.Program.User)
	if e != nil {
		return ""
	}
	principal := ""
	switch u.Kind {
	case "windows":
		principal = u.SID
	case "posix":
		if u.UID >= 0 {
			principal = strconv.Itoa(u.UID)
		}
	}
	if principal == "" || principal != owner {
		return ""
	}
	path, e := peer.Path.AtLeast(listen.Program.Path)
	if e != nil || !filepath.IsAbs(path) {
		return ""
	}
	raw, _ := json.Marshal([]string{"owner-program@1", u.Kind, principal, filepath.Clean(path)})
	return string(raw)
}
