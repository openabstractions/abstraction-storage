package service

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"io/fs"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"time"

	casapi "github.com/openabstractions/abstraction-cas/go/api"
	identity "github.com/openabstractions/abstraction-identity"
	storage "github.com/openabstractions/abstraction-storage/go"
	api "github.com/openabstractions/abstraction-storage/go/abstraction/storage/content"
	contentdigest "github.com/openabstractions/abstraction-storage/go/digest"
	"sync"
)

const (
	RemovalAccount    = "account"
	HoldsSelf         = "self"
	maxRemovalTargets = 4096
)

type RemovalOptions struct {
	// Gate admits the bound Panel and decides its account and exact store right.
	Gate  Policy
	Holds Policy
	// Sources includes every accepted source, including one that is currently
	// unavailable. An unavailable source supplies no Reader.
	Sources Sources
	// Observed approves an exact observed-only target using a retained answer.
	Observed func(context.Context, *identity.Peer, string, string) error
	// InlineAnswer is the runtime Asks owner for an authenticated Panel
	// click. It records and checks exactly one occurrence under native custody.
	InlineAnswer func(ctx context.Context, peer *identity.Peer, store, digest, observation, occurrence string, size int64) error
	// WithoutLease serializes current lease admission and the native effect.
	WithoutLease       func(context.Context, string, func() error) error
	Records            casapi.Store
	RecordPath         string
	Grace              time.Duration
	Removed            func(digest string, size int64)
	CompleteInline     func(context.Context, string) error
	ArchiveRestore     ArchiveRestoreHandler
	ArchiveRestoreWork func(context.Context, ArchivePlacement, ArchiveRelease)
}

type removal struct {
	mu      sync.Mutex
	store   *storage.ContentStore
	options RemovalOptions
	base    casapi.Value
	data    removalRecords
	first   map[string]removalObservation
	seal    [32]byte
	seen    map[string]bool
	fenced  bool
	now     func() time.Time
}

type removalRecords struct {
	Version  int
	Holds    map[string]declaredHold
	Observed map[string]bool
}

type removalObservation struct {
	at      time.Time
	proof   storage.RemovalProof
	sources []string
}

func (h *Component) EnableRemoval(options RemovalOptions) error {
	h.lifecycle.Lock()
	defer h.lifecycle.Unlock()
	if h.serving || h.ctx.Err() != nil || h.removals != nil {
		return errors.New("storage: configure removal once before Serve")
	}
	store, ok := h.registry.store.(*storage.ContentStore)
	if !ok || options.Gate == nil || options.Holds == nil || options.Sources == nil || options.Observed == nil && options.InlineAnswer == nil ||
		options.WithoutLease == nil || options.Records == nil || !filepath.IsAbs(options.RecordPath) || options.Grace <= 0 {
		return errors.New("storage: removal requires the owning store, verification, policies and records")
	}
	r := &removal{store: store, options: options, now: time.Now, first: make(map[string]removalObservation), seen: make(map[string]bool)}
	if options.InlineAnswer != nil && options.CompleteInline == nil {
		return errors.New("storage: inline removal requires a completion owner")
	}
	if _, err := rand.Read(r.seal[:]); err != nil {
		return err
	}
	base, err := options.Records.Read(options.RecordPath)
	if err != nil {
		return err
	}
	r.base = base
	r.data = removalRecords{Version: 1, Holds: make(map[string]declaredHold), Observed: make(map[string]bool)}
	if base.Data != nil {
		var data removalRecords
		if len(base.Data) > MaxRecordFileBytes || decodeRecord(base.Data, &data) != nil || !data.valid() {
			return errors.New("storage: invalid removal records")
		}
		r.data = data
	}
	h.removals = r
	return nil
}

func (r *removal) saveLocked() error {
	data, err := json.Marshal(r.data)
	if err != nil {
		return err
	}
	if len(data) > MaxRecordFileBytes {
		return errors.New("storage: removal records exceed limit")
	}
	if err := r.options.Records.Write(r.options.RecordPath, r.base, data); err != nil {
		r.fenced = true
		return err
	}
	r.base = casapi.Value{Data: data}
	return nil
}

type removalReceiver struct {
	removal  *removal
	registry *registry
	writers  *writers
	scope    string
	peer     *identity.Peer
	ctx      context.Context
}

func (c removalReceiver) Remove(target string) (api.RemoveResult, error) {
	if c.removal.options.InlineAnswer == nil {
		return c.checkFor(target, true, removalTicket{}, nil)
	}
	ticket, ok := c.openTicket(target)
	if !ok {
		return api.RemoveResult{Outcome: api.RemoveOutcomeForbidden, Holders: []api.Holder{}}, nil
	}
	return c.checkFor(ticket.digest, true, ticket, nil)
}

type removalEligibility struct {
	proof      storage.RemovalProof
	needsAdmin bool
}

func (c removalReceiver) checkFor(target string, remove bool, ticket removalTicket, eligible *removalEligibility) (api.RemoveResult, error) {
	result := func(outcome api.RemoveOutcome) (api.RemoveResult, error) {
		return api.RemoveResult{Outcome: outcome, Holders: []api.Holder{}}, nil
	}
	r := c.removal
	if status := decide(c.ctx, c.scope, r.options.Gate, c.peer, RemovalAccount); status != "" {
		return result(removalGateOutcome(status))
	}
	if !contentdigest.Valid(target) {
		return result(api.RemoveOutcomeInvalid)
	}
	resource := "store:" + r.store.Name()
	if status := decide(c.ctx, c.scope, r.options.Gate, c.peer, resource); status != "" {
		return result(removalGateOutcome(status))
	}
	proof, err := r.store.ObserveRemoval(c.ctx, target)
	if errors.Is(err, fs.ErrNotExist) {
		return result(api.RemoveOutcomeNotFound)
	}
	if err != nil {
		return result(api.RemoveOutcomeUnavailable)
	}
	if remove && r.options.InlineAnswer != nil && (ticket.observation != proof.Binding() || ticket.expired(r.now())) {
		return result(api.RemoveOutcomeForbidden)
	}
	started := r.now()
	sources, observed, err := observeRemoval(c.ctx, r.options.Sources(c.ctx), target)
	r.mu.Lock()
	defer r.mu.Unlock()
	if err != nil || c.ctx.Err() != nil || r.fenced {
		delete(r.first, target)
		return result(api.RemoveOutcomeUnavailable)
	}
	now := r.now()
	if len(observed) != 0 {
		if !r.seen[target] && len(r.seen) >= maxRemovalTargets {
			r.fenced = true
			return result(api.RemoveOutcomeUnavailable)
		}
		r.seen[target] = true
	}
	if remove && r.seen[target] && !r.data.Observed[target] {
		if len(r.data.Observed) >= maxRemovalTargets {
			return result(api.RemoveOutcomeUnavailable)
		}
		r.data.Observed[target] = true
		if err := r.saveLocked(); err != nil {
			delete(r.data.Observed, target)
			return result(api.RemoveOutcomeUnavailable)
		}
	}
	holders := append(r.holdersLocked(target, now), observed...)
	if len(holders) != 0 {
		delete(r.first, target)
		return api.RemoveResult{Outcome: api.RemoveOutcomeHeld, Holders: holders}, nil
	}
	first, exists := r.first[target]
	if !exists || first.proof != proof || !slices.Equal(first.sources, sources) || now.Sub(first.at) > 10*r.options.Grace {
		for digest, observation := range r.first {
			if now.Sub(observation.at) > 10*r.options.Grace {
				delete(r.first, digest)
			}
		}
		if len(r.first) < maxRemovalTargets {
			r.first[target] = removalObservation{at: now, proof: proof, sources: sources}
		}
		return result(api.RemoveOutcomeUnavailable)
	}
	if started.Sub(first.at) < r.options.Grace {
		return result(api.RemoveOutcomeUnavailable)
	}
	if r.options.InlineAnswer == nil && (r.data.Observed[target] || r.seen[target]) {
		if !remove {
			return result(api.RemoveOutcomeUnavailable)
		}
		if status := policyStatus(c.ctx, r.options.Observed(c.ctx, c.peer, resource, target)); status != "" {
			return result(removalGateOutcome(status))
		}
	}
	if status := decide(c.ctx, c.scope, r.options.Gate, c.peer, RemovalAccount); status != "" {
		return result(removalGateOutcome(status))
	}
	if status := decide(c.ctx, c.scope, r.options.Gate, c.peer, resource); status != "" {
		return result(removalGateOutcome(status))
	}
	if err := c.withoutLocalLease(target, func() error {
		return r.options.WithoutLease(c.ctx, target, func() error {
			if !remove {
				if r.options.InlineAnswer != nil && !proof.CanRetire() {
					return storage.ErrRemovalUnsupported
				}
				return r.store.CheckObservedExclusive(c.ctx, proof)
			}
			if r.options.InlineAnswer == nil {
				return r.store.RemoveObserved(c.ctx, proof)
			}
			return r.store.RetireObserved(c.ctx, proof, ticket.occurrence, func() error {
				if ticket.expired(r.now()) {
					return ErrInlineRemovalRefused
				}
				for _, scope := range []string{RemovalAccount, resource} {
					if decide(c.ctx, c.scope, r.options.Gate, c.peer, scope) != "" {
						return ErrInlineRemovalRefused
					}
				}
				if ticket.expired(r.now()) {
					return ErrInlineRemovalRefused
				}
				if err := r.options.InlineAnswer(c.ctx, c.peer, r.store.Name(), target, proof.Binding(), ticket.occurrence, proof.Size()); err != nil {
					return err
				}
				for _, scope := range []string{RemovalAccount, resource} {
					if decide(c.ctx, c.scope, r.options.Gate, c.peer, scope) != "" {
						return ErrInlineRemovalRefused
					}
				}
				if ticket.expired(r.now()) {
					return ErrInlineRemovalRefused
				}
				return nil
			})
		})
	}); err != nil {
		switch {
		case errors.Is(err, storage.ErrRemovalBusy):
			return result(api.RemoveOutcomeBusy)
		case errors.Is(err, storage.ErrRemovalNeedsAdministrator):
			if eligible != nil {
				eligible.needsAdmin = true
			}
			return result(api.RemoveOutcomeForbidden)
		case errors.Is(err, ErrInlineRemovalRefused):
			return result(api.RemoveOutcomeForbidden)
		case errors.Is(err, storage.ErrRemovalUnsupported):
			return result(api.RemoveOutcomeUnsupported)
		default:
			delete(r.first, target)
			return result(api.RemoveOutcomeUnavailable)
		}
	}
	if !remove {
		if eligible != nil {
			eligible.proof = proof
		}
		return result(api.RemoveOutcomeRemoved)
	}
	delete(r.first, target)
	if r.options.InlineAnswer != nil {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(c.ctx), 5*time.Second)
		defer cancel()
		err := r.store.CompleteRetiredRemoval(ctx, target, proof.Binding(), ticket.occurrence, func() error {
			return r.options.CompleteInline(ctx, ticket.occurrence)
		})
		if err != nil {
			return result(api.RemoveOutcomeUnavailable)
		}
	}
	if r.options.Removed != nil {
		r.options.Removed(target, proof.Size())
	}
	return result(api.RemoveOutcomeRemoved)
}

func (c removalReceiver) withoutLocalLease(target string, effect func() error) error {
	if c.writers != nil {
		c.writers.mu.Lock()
		defer c.writers.mu.Unlock()
		if c.writers.digests[target] != nil {
			return storage.ErrRemovalBusy
		}
	}
	r := c.registry
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return errors.New("storage: reader admission closed")
	}
	if r.pendingDigest[target] != 0 {
		return storage.ErrRemovalBusy
	}
	for _, resource := range r.resources {
		if resource.digest == target {
			return storage.ErrRemovalBusy
		}
	}
	return effect()
}

func removalGateOutcome(status string) api.RemoveOutcome {
	if status == "unavailable" {
		return api.RemoveOutcomeUnavailable
	}
	return api.RemoveOutcomeForbidden
}

func observeRemoval(ctx context.Context, designated []DesignatedSource, target string) ([]string, []api.Holder, error) {
	var sources []string
	var holders []api.Holder
	seen := make(map[string]bool)
	if len(designated) == 0 {
		return nil, nil, errInventoryUnavailable
	}
	for _, source := range designated {
		if ctx.Err() != nil || source.Reader == nil || source.Program == "" || len(source.Stores) == 0 {
			return nil, nil, errInventoryUnavailable
		}
		page, err := source.Reader.Verify(target)
		if err != nil || page.Outcome != api.SourcePageOutcomePage || !page.Complete || page.Continuation != "" {
			return nil, nil, errInventoryUnavailable
		}
		reported := make(map[string]api.Store, len(page.Stores))
		for _, candidate := range page.Stores {
			if _, duplicate := reported[candidate.Name]; duplicate || candidate.Name == "" {
				return nil, nil, errInventoryUnavailable
			}
			reported[candidate.Name] = candidate
		}
		for _, name := range source.Stores {
			if seen[name] {
				return nil, nil, errInventoryUnavailable
			}
			seen[name] = true
			s, answered := reported[name]
			if !answered {
				return nil, nil, errInventoryUnavailable
			}
			if s.Placement != api.PlacementLocal || s.Domain != "" || len(s.Errors) != 0 {
				return nil, nil, errInventoryUnavailable
			}
			sources = append(sources, source.Program+"\x00"+s.Name+"\x00"+s.Locator+"\x00"+strconv.FormatBool(s.Present))
		}
		inspect := func(holds []api.Hold) error {
			for _, hold := range holds {
				if hold.Basis == nil || hold.Basis.Index == "" || hold.Basis.Key == "" || hold.Holder.Program == "" {
					return errInventoryUnavailable
				}
				holder := hold.Holder
				holder.EstablishedBy = source.Program
				holders = append(holders, holder)
				if len(holders) > MaxInventoryRecords {
					return errInventoryUnavailable
				}
			}
			return nil
		}
		for _, m := range page.Manifests {
			if slices.Contains(source.Stores, m.Manifest.Store) {
				if err := inspect(m.Holds); err != nil {
					return nil, nil, err
				}
			}
		}
		for _, o := range page.Objects {
			if slices.Contains(source.Stores, o.Store) {
				if err := inspect(o.Holds); err != nil {
					return nil, nil, err
				}
			}
		}
		for _, dangling := range page.Dangling {
			if dangling.Holder.Program == "" || dangling.Basis.Index == "" || dangling.Basis.Key == "" {
				return nil, nil, errInventoryUnavailable
			}
			holder := dangling.Holder
			holder.EstablishedBy = source.Program
			holders = append(holders, holder)
			if len(holders) > MaxInventoryRecords {
				return nil, nil, errInventoryUnavailable
			}
		}
	}
	sort.Strings(sources)
	return sources, holders, nil
}
