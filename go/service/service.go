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
	registry  *registry
	writers   *writers
	changes   *changeJournal
	interval  time.Duration
	listener  listen.Listener
	owner     string
	ctx       context.Context
	cancel    context.CancelFunc
	once      sync.Once
	lifecycle sync.Mutex
	serving   bool
	workers   sync.WaitGroup
	slots     chan struct{}
	OnError   func(error)
	OnStopped func()
}

// Listen owns no Store configuration. Policy is mandatory and never replaced by
// a same-account blanket grant. Configure callbacks before Serve.
func Listen(endpoint string, store storage.Store, policy Policy) (*Host, error) {
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
	l, e := listen.Listen(endpoint)
	if e != nil {
		return nil, e
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &Host{registry: newRegistry(store, policy), listener: l, owner: owner.Uid, ctx: ctx, cancel: cancel, slots: make(chan struct{}, 32)}, nil
}
func (h *Host) Close() error {
	var e error
	h.once.Do(func() {
		h.cancel()
		e = h.listener.Close()
		h.registry.close()
		h.lifecycle.Lock()
		writers, changes := h.writers, h.changes
		h.lifecycle.Unlock()
		if writers != nil {
			writers.close()
		}
		if changes != nil {
			changes.close()
		}
	})
	return e
}

// EnableChanges adds abstraction.storage/content-changes@1 to this endpoint.
// observe authorizes each Observe and List call on ChangesResource; the read
// policy filters every change and listed object per digest. The present objects
// of a Lister provider are recorded without being journaled, and the provider is
// polled every interval for external additions and deletions. capacity bounds
// the journal. Configure it before Serve.
func (h *Host) EnableChanges(observe Policy, interval time.Duration, capacity int) error {
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
func (h *Host) ChangesAvailable() bool {
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
func (h *Host) EnableWriter(policy Policy, limit int64, records casapi.Store, recordPath string) error {
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
func (h *Host) WriterAvailable() bool {
	h.lifecycle.Lock()
	defer h.lifecycle.Unlock()
	return h.writers != nil && h.ctx.Err() == nil
}
func (h *Host) Serve(ctx context.Context) error {
	h.lifecycle.Lock()
	if h.serving {
		h.lifecycle.Unlock()
		return errors.New("storage: host already served")
	}
	h.serving = true
	writers, changes, interval := h.writers, h.changes, h.interval
	h.lifecycle.Unlock()
	if writers != nil && changes != nil {
		writers.mu.Lock()
		writers.onCommitted = changes.committed
		writers.mu.Unlock()
	}
	if changes != nil {
		h.workers.Add(1)
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
	stop := context.AfterFunc(ctx, func() { h.Close() })
	defer stop()
	defer h.workers.Wait()
	defer h.Close()
	defer func() {
		if h.OnStopped != nil {
			h.OnStopped()
		}
	}()
	h.workers.Add(1)
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
			}
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
			// Change observation may wait up to 30 seconds; other calls finish well within this budget.
			callCtx, cancel := context.WithTimeout(h.ctx, 35*time.Second)
			defer cancel()
			call, e := listen.ReceiveFramed(callCtx, conn, listen.Program, MaxFrameBytes)
			if call != nil {
				defer call.Close()
			}
			if e == nil {
				peer, proofErr := call.Peer()
				scope := ""
				if proofErr == nil {
					scope = callerScope(peer, h.owner)
				}
				var reply []byte
				if service, nameErr := api.ServiceName(call.Frame); nameErr == nil && service == "abstraction.storage/content-changes@1" && changes != nil {
					dispatcher := api.ContentChangesDispatcher{Handler: changesReceiver{journal: changes, scope: scope, peer: peer, ctx: callCtx, wait: call.WaitContext()}}
					reply, e = dispatcher.ExchangeFrame(call.Frame)
				} else if nameErr == nil && service == "abstraction.storage/content-writer@1" && writers != nil {
					dispatcher := api.ContentWriterDispatcher{Handler: writeReceiver{writers: writers, scope: scope, peer: peer, ctx: callCtx}}
					reply, e = dispatcher.ExchangeFrame(call.Frame)
				} else {
					// The reader dispatcher refuses unknown or unconfigured services.
					dispatcher := api.ContentReaderDispatcher{Handler: receiver{registry: h.registry, scope: scope, peer: peer, ctx: callCtx}}
					reply, e = dispatcher.ExchangeFrame(call.Frame)
				}
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
