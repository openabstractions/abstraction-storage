package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"strings"
	"time"

	identity "github.com/openabstractions/abstraction-identity"
	api "github.com/openabstractions/abstraction-storage/go/abstraction/storage/content"
	contentdigest "github.com/openabstractions/abstraction-storage/go/digest"
)

const maxHoldLeaseMs = 2592000000
const holdAuditRetention = 24 * time.Hour

type declaredHold struct {
	Request    string
	Owner      nativeOwner
	Hold       api.Hold
	LeaseMs    int64
	ReleasedAt int64
}

func (h declaredHold) end() time.Time {
	if h.ReleasedAt != 0 {
		return time.UnixMilli(h.ReleasedAt)
	}
	if h.Hold.Lifetime == api.LifetimeLease {
		end, _ := time.Parse(time.RFC3339Nano, h.Hold.Expires)
		return end
	}
	return time.Time{}
}

func (h declaredHold) active(now time.Time) bool {
	end := h.end()
	return end.IsZero() || now.Before(end)
}

func holdLifetimeValid(lifetime api.Lifetime, leaseMs int64) bool {
	switch lifetime {
	case api.LifetimeLease:
		return leaseMs >= 1 && leaseMs <= maxHoldLeaseMs
	case api.LifetimeUntilReleased, api.LifetimeWhilePresent:
		return leaseMs == 0
	default:
		return false
	}
}

func (d removalRecords) valid() bool {
	if d.Version != 1 || d.Holds == nil || len(d.Holds) > maxRemovalTargets || d.Observed == nil || len(d.Observed) > maxRemovalTargets {
		return false
	}
	for digest, observed := range d.Observed {
		if !observed || !contentdigest.Valid(digest) {
			return false
		}
	}
	for id, h := range d.Holds {
		if len(id) != 48 || h.Hold.ID != id || !h.Owner.valid(h.Owner.scope()) || !validRequest(h.Request) ||
			!contentdigest.Valid(h.Hold.Target) || !holdPurposeValid(h.Hold.Purpose) ||
			!holdLifetimeValid(h.Hold.Lifetime, h.LeaseMs) || h.Hold.Holder != holderForOwner(h.Owner) ||
			h.Hold.Attestation != api.AttestationDeclared || h.Hold.Basis != nil || h.ReleasedAt < 0 {
			return false
		}
		if _, err := hex.DecodeString(id); err != nil {
			return false
		}
		if _, err := time.Parse(time.RFC3339Nano, h.Hold.ObservedAt); err != nil {
			return false
		}
		if h.Hold.Lifetime == api.LifetimeLease {
			if _, err := time.Parse(time.RFC3339Nano, h.Hold.Expires); err != nil {
				return false
			}
		} else if h.Hold.Expires != "" {
			return false
		}
	}
	return true
}

func (r *removal) holdersLocked(target string, now time.Time) []api.Holder {
	var holders []api.Holder
	for _, h := range r.data.Holds {
		if h.Hold.Target == target && h.active(now) {
			holders = append(holders, h.Hold.Holder)
		}
	}
	return holders
}

func (r *removal) pruneHoldsLocked(now time.Time) {
	for id, h := range r.data.Holds {
		if end := h.end(); !end.IsZero() && now.Sub(end) > holdAuditRetention {
			delete(r.data.Holds, id)
		}
	}
}

func holderForOwner(n nativeOwner) api.Holder {
	return api.Holder{Account: n.Account, Program: n.Program, Instance: n.Program}
}

func holdPurposeValid(purpose string) bool {
	return len(purpose) > 0 && len(purpose) <= 256 && !strings.ContainsAny(purpose, "\x00\r\n")
}

type holdsReceiver struct {
	removal *removal
	scope   string
	peer    *identity.Peer
	ctx     context.Context
}

func (c holdsReceiver) own() (nativeOwner, string) {
	binding := nativeBindingOf(c.ctx)
	if binding == nil || binding.owner.scope() != c.scope {
		return nativeOwner{}, "forbidden"
	}
	return binding.owner, decide(c.ctx, c.scope, c.removal.options.Holds, c.peer, HoldsSelf)
}

func (c holdsReceiver) Hold(request, target, purpose string, lifetime api.Lifetime, leaseMs int64) (api.HoldResult, error) {
	if !validRequest(request) || !contentdigest.Valid(target) || !holdPurposeValid(purpose) || !holdLifetimeValid(lifetime, leaseMs) {
		return api.HoldResult{Outcome: api.HoldOutcomeInvalid}, nil
	}
	owner, status := c.own()
	if status != "" {
		outcome, _ := api.ParseHoldOutcome(status)
		return api.HoldResult{Outcome: outcome}, nil
	}
	r := c.removal
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.fenced || c.ctx.Err() != nil {
		return api.HoldResult{Outcome: api.HoldOutcomeUnavailable}, nil
	}
	now := r.now()
	r.pruneHoldsLocked(now)
	for _, existing := range r.data.Holds {
		if !existing.Owner.sameProgram(owner) || existing.Request != request {
			continue
		}
		if existing.Hold.Target != target || existing.Hold.Purpose != purpose || existing.Hold.Lifetime != lifetime || existing.LeaseMs != leaseMs {
			return api.HoldResult{Outcome: api.HoldOutcomeConflict}, nil
		}
		return api.HoldResult{Outcome: api.HoldOutcomeHeld, Hold: &existing.Hold}, nil
	}
	if len(r.data.Holds) >= maxRemovalTargets {
		return api.HoldResult{Outcome: api.HoldOutcomeExhausted}, nil
	}
	if _, found := r.store.Find(target); !found {
		return api.HoldResult{Outcome: api.HoldOutcomeUnknownTarget}, nil
	}
	var random [24]byte
	if _, err := rand.Read(random[:]); err != nil {
		return api.HoldResult{Outcome: api.HoldOutcomeUnavailable}, nil
	}
	id := hex.EncodeToString(random[:])
	held := api.Hold{ID: id, Holder: holderForOwner(owner), Target: target, Purpose: purpose, Lifetime: lifetime,
		Attestation: api.AttestationDeclared, ObservedAt: now.UTC().Format(time.RFC3339Nano)}
	if lifetime == api.LifetimeLease {
		held.Expires = now.Add(time.Duration(leaseMs) * time.Millisecond).UTC().Format(time.RFC3339Nano)
	}
	r.data.Holds[id] = declaredHold{Request: request, Owner: owner, Hold: held, LeaseMs: leaseMs}
	if err := r.saveLocked(); err != nil {
		delete(r.data.Holds, id)
		return api.HoldResult{Outcome: api.HoldOutcomeUnavailable}, nil
	}
	delete(r.first, target)
	return api.HoldResult{Outcome: api.HoldOutcomeHeld, Hold: &held}, nil
}

func (c holdsReceiver) Release(id string) (api.ReleaseResult, error) {
	if len(id) != 48 {
		return api.ReleaseResult{Outcome: api.ReleaseOutcomeInvalid}, nil
	}
	owner, status := c.own()
	if status != "" {
		outcome, _ := api.ParseReleaseOutcome(status)
		return api.ReleaseResult{Outcome: outcome}, nil
	}
	r := c.removal
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.fenced || c.ctx.Err() != nil {
		return api.ReleaseResult{Outcome: api.ReleaseOutcomeUnavailable}, nil
	}
	now := r.now()
	r.pruneHoldsLocked(now)
	held, exists := r.data.Holds[id]
	if !exists || !held.Owner.sameProgram(owner) {
		return api.ReleaseResult{Outcome: api.ReleaseOutcomeUnknown}, nil
	}
	if held.active(now) {
		held.ReleasedAt = now.UnixMilli()
		r.data.Holds[id] = held
		delete(r.first, held.Hold.Target)
		if err := r.saveLocked(); err != nil {
			return api.ReleaseResult{Outcome: api.ReleaseOutcomeUnavailable}, nil
		}
	}
	return api.ReleaseResult{Outcome: api.ReleaseOutcomeReleased}, nil
}

func (c holdsReceiver) Renew(id string, leaseMs int64) (api.RenewResult, error) {
	if len(id) != 48 || leaseMs < 1 || leaseMs > maxHoldLeaseMs {
		return api.RenewResult{Outcome: api.RenewOutcomeInvalid}, nil
	}
	owner, status := c.own()
	if status != "" {
		outcome, _ := api.ParseRenewOutcome(status)
		return api.RenewResult{Outcome: outcome}, nil
	}
	r := c.removal
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.fenced || c.ctx.Err() != nil {
		return api.RenewResult{Outcome: api.RenewOutcomeUnavailable}, nil
	}
	now := r.now()
	r.pruneHoldsLocked(now)
	held, exists := r.data.Holds[id]
	if !exists || !held.Owner.sameProgram(owner) || held.ReleasedAt != 0 {
		return api.RenewResult{Outcome: api.RenewOutcomeUnknown}, nil
	}
	if held.Hold.Lifetime != api.LifetimeLease {
		return api.RenewResult{Outcome: api.RenewOutcomeInvalid}, nil
	}
	if !held.active(now) {
		return api.RenewResult{Outcome: api.RenewOutcomeExpired}, nil
	}
	held.Hold.Expires = now.Add(time.Duration(leaseMs) * time.Millisecond).UTC().Format(time.RFC3339Nano)
	r.data.Holds[id] = held
	if err := r.saveLocked(); err != nil {
		return api.RenewResult{Outcome: api.RenewOutcomeUnavailable}, nil
	}
	return api.RenewResult{Outcome: api.RenewOutcomeRenewed, Hold: &held.Hold}, nil
}
