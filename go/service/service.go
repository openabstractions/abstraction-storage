// Package service adapts explicitly configured storage providers to shared IPC.
package service

import (
	"context"
	"encoding/json"
	"errors"
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
	h.once.Do(func() { h.cancel(); e = h.listener.Close(); h.registry.close() })
	return e
}
func (h *Host) Serve(ctx context.Context) error {
	h.lifecycle.Lock()
	if h.serving {
		h.lifecycle.Unlock()
		return errors.New("storage: host already served")
	}
	h.serving = true
	h.lifecycle.Unlock()
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
			callCtx, cancel := context.WithTimeout(h.ctx, 5*time.Second)
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
				handler := receiver{registry: h.registry, scope: scope, peer: peer, ctx: callCtx}
				dispatcher := api.ContentReaderDispatcher{Handler: handler}
				var reply []byte
				reply, e = dispatcher.ExchangeFrame(call.Frame)
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
