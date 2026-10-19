package service

import (
	"crypto/hmac"
	"encoding/hex"
	"sort"
	"strconv"
	"strings"
	"time"

	storage "github.com/openabstractions/abstraction-storage/go"
	api "github.com/openabstractions/abstraction-storage/go/abstraction/storage/content"
	contentdigest "github.com/openabstractions/abstraction-storage/go/digest"
)

// RemovalNeedsAdministrator is a private catalogue status for a proved file
// whose native deletion access was refused.
const RemovalNeedsAdministrator = "needs administrator"

// RemovalInventoryGuarantee distinguishes the private operator catalogue from
// declared provider sources. Its admission remains at the receiving service.
const RemovalInventoryGuarantee = "openabstractions.storage.removal-inventory"

// removalSource exposes a bounded native-store listing to the bound operator
// over the existing InventorySource codec. Public Inventory uses its own
// unchanged designated source list.
type removalSource struct{ removalReceiver }

func (s removalSource) access() string {
	o := s.removal.options
	if status := decide(s.ctx, s.scope, o.Gate, s.peer, RemovalAccount); status != "" {
		return status
	}
	return decide(s.ctx, s.scope, o.Gate, s.peer, "store:"+s.removal.store.Name())
}

func (s removalSource) sourceStore() api.Store {
	return api.Store{Name: s.removal.store.Name(), Program: "openabstractions", Locator: s.removal.store.Name(),
		Rule: "committed runtime content", Present: true, Errors: []api.StoreError{}, Placement: api.PlacementLocal}
}

func (s removalSource) Describe() (api.SourceDescription, error) {
	out := api.SourceDescription{Stores: []api.Store{}, Schemes: []string{}, Capabilities: []string{}}
	switch s.access() {
	case "":
		out.Outcome = api.SourceDescriptionOutcomeDescribed
		out.Name = s.removal.store.Name()
		out.Stores = append(out.Stores, s.sourceStore())
		out.Capabilities = []string{"snapshot", "verify"}
	case "unavailable":
		out.Outcome = api.SourceDescriptionOutcomeUnavailable
	default:
		out.Outcome = api.SourceDescriptionOutcomeForbidden
	}
	return out, nil
}

func removalSourcePage(outcome api.SourcePageOutcome) api.SourcePage {
	return api.SourcePage{Outcome: outcome, Stores: []api.Store{}, Manifests: []api.ManifestHolders{},
		Objects: []api.ObjectHolders{}, Dangling: []api.Dangling{}, Changes: []api.SourceChange{}}
}

func removalSourceObject(name, digest string, size int64, modified time.Time) api.ObjectHolders {
	return api.ObjectHolders{Store: name, Holds: []api.Hold{}, Object: api.Object{
		Locator: digest, Digest: digest, Size: size, Evidence: api.EvidenceNamed, Modified: modified.Format(time.RFC3339Nano),
	}}
}

func (s removalSource) Snapshot(continuation string, limit int64) (api.SourcePage, error) {
	if len(continuation) > 160 || limit < 1 || limit > MaxInventoryPage {
		return removalSourcePage(api.SourcePageOutcomeInvalid), nil
	}
	switch s.access() {
	case "forbidden":
		return removalSourcePage(api.SourcePageOutcomeForbidden), nil
	case "unavailable":
		return removalSourcePage(api.SourcePageOutcomeUnavailable), nil
	}
	after, valid := s.readCursor(continuation)
	if !valid {
		return removalSourcePage(api.SourcePageOutcomeInvalid), nil
	}
	refs, err := s.removal.store.List(MaxInventoryRecords)
	if err != nil {
		return removalSourcePage(api.SourcePageOutcomeUnavailable), nil
	}
	sort.Slice(refs, func(i, j int) bool { return removalObjectBefore(refs[i], refs[j]) })
	if after.Digest != "" {
		first := sort.Search(len(refs), func(i int) bool { return removalObjectBefore(after, refs[i]) })
		refs = refs[first:]
	}
	page := removalSourcePage(api.SourcePageOutcomePage)
	page.Stores = append(page.Stores, s.sourceStore())
	page.Complete = len(refs) <= int(limit)
	refs = refs[:min(len(refs), int(limit))]
	if !page.Complete {
		page.Continuation = s.cursor(refs[len(refs)-1])
	}
	for _, ref := range refs {
		proof, err := s.removal.store.ObserveRemoval(s.ctx, ref.Digest)
		if err != nil {
			return removalSourcePage(api.SourcePageOutcomeUnavailable), nil
		}
		page.Objects = append(page.Objects, removalSourceObject(ref.Store, ref.Digest, proof.Size(), proof.Modified()))
	}
	return page, nil
}

func removalObjectBefore(a, b storage.Ref) bool {
	return a.Size > b.Size || a.Size == b.Size && a.Digest < b.Digest
}

func (s removalSource) cursor(after storage.Ref) string {
	body := strconv.FormatInt(after.Size, 10) + "." + after.Digest
	return body + "." + hex.EncodeToString(s.cursorMAC(body))
}

func (s removalSource) cursorMAC(body string) []byte {
	return s.removal.signTicket("inventory\x00" + s.scope + "\x00" + body)
}

func (s removalSource) readCursor(raw string) (storage.Ref, bool) {
	if raw == "" {
		return storage.Ref{}, true
	}
	parts := strings.Split(raw, ".")
	if len(parts) != 3 || !contentdigest.Valid(parts[1]) || len(parts[2]) != 64 {
		return storage.Ref{}, false
	}
	size, err := strconv.ParseInt(parts[0], 10, 64)
	mac, macErr := hex.DecodeString(parts[2])
	if err != nil || size < 0 || macErr != nil || !hmac.Equal(mac, s.cursorMAC(parts[0]+"."+parts[1])) {
		return storage.Ref{}, false
	}
	return storage.Ref{Digest: parts[1], Size: size}, true
}

// Verify offers a candidate only after the same two-observation, hold, lease
// and sharing checks as Remove. It never marks the file for deletion.
func (s removalSource) Verify(target string) (api.SourcePage, error) {
	if !contentdigest.Valid(target) {
		return removalSourcePage(api.SourcePageOutcomeInvalid), nil
	}
	var eligible removalEligibility
	result, err := s.checkFor(target, false, removalTicket{}, &eligible)
	if err != nil {
		return removalSourcePage(api.SourcePageOutcomeUnavailable), err
	}
	switch result.Outcome {
	case api.RemoveOutcomeForbidden:
		page := removalSourcePage(api.SourcePageOutcomeForbidden)
		if eligible.needsAdmin {
			store := s.sourceStore()
			store.Rule = RemovalNeedsAdministrator
			page.Stores = append(page.Stores, store)
		}
		return page, nil
	case api.RemoveOutcomeRemoved:
		if eligible.proof == (storage.RemovalProof{}) {
			return removalSourcePage(api.SourcePageOutcomeUnavailable), nil
		}
		page := removalSourcePage(api.SourcePageOutcomePage)
		page.Stores = append(page.Stores, s.sourceStore())
		object := removalSourceObject(s.removal.store.Name(), target, eligible.proof.Size(), eligible.proof.Modified())
		if s.removal.options.InlineAnswer != nil {
			ticket, err := s.issueTicket(target, eligible.proof.Binding())
			if err != nil {
				return removalSourcePage(api.SourcePageOutcomeUnavailable), err
			}
			object.Object.Locator = ticket
		}
		page.Objects = append(page.Objects, object)
		page.Complete = true
		return page, nil
	default:
		return removalSourcePage(api.SourcePageOutcomeUnavailable), nil
	}
}

func (s removalSource) Observe(string, int64, int64) (api.SourcePage, error) {
	return removalSourcePage(api.SourcePageOutcomeUnavailable), nil
}

// The provider-shaped read path has no destructive authority.
func (s removalSource) Remove(string) (api.RemoveResult, error) {
	return api.RemoveResult{Outcome: api.RemoveOutcomeUnsupported, Holders: []api.Holder{}}, nil
}
