package service

import (
	"context"
	"errors"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	identity "github.com/openabstractions/abstraction-identity"
	api "github.com/openabstractions/abstraction-storage/go/abstraction/storage/content"
)

const (
	// MaxInventoryPage bounds combined manifests, stray objects and dangling references.
	MaxInventoryPage = 256
	// MaxInventoryRecords bounds one frozen composition across every source.
	MaxInventoryRecords = 65536
	// InventoryResource is the resource the inventory.read policy receives.
	InventoryResource = "abstraction.storage/inventory"
)

var (
	errInventoryLate        = errors.New("storage: configure the inventory composition before Serve")
	errInventoryUnsupported = errors.New("storage: the inventory composition requires an explicit read gate and designated sources")
	errInventoryUnavailable = errors.New("storage: a designated inventory source did not answer")
)

// InventorySourceReader is one designated source, as the generated
// InventorySource client serves it. The composition reads; it never removes.
type InventorySourceReader interface {
	Snapshot(continuation string, limit int64) (api.SourcePage, error)
	Verify(target string) (api.SourcePage, error)
}

// DesignatedSource is one source the runtime accepted, with the stores its
// rule permits. Records of any other store are dropped: acceptance is per
// store, and a source that reports one it was not given reports nothing here.
// Program is the bound source program the composition writes into every
// holder's established_by.
type DesignatedSource struct {
	Program string
	Stores  []string
	Reader  InventorySourceReader
}

// Sources is the designated sources at call time. The runtime supplies it;
// the composition never discovers a source by itself.
type Sources func(context.Context) []DesignatedSource

// record is one composed inventory record in page order: manifests, then
// stray objects, then dangling references.
type record struct {
	manifest *api.ManifestHolders
	object   *api.ObjectHolders
	dangling *api.Dangling
	// digests is what the read policy decides this record by; empty means the
	// record carries no digest and passes unfiltered.
	digests []string
}

type inventorySnapshot struct {
	stores  []api.Store
	records []record
	cursor  string
	scope   string
	expires time.Time
}

// inventory composes abstraction.storage/inventory@1 from designated sources.
// It owns freezing, paging, store acceptance and the holder rewrite the
// contract requires; everything else is what a source supplied.
type inventory struct {
	mu        sync.Mutex
	read      Policy
	gate      Policy
	sources   Sources
	cursor    func() string
	snapshots map[string]*inventorySnapshot
	now       func() time.Time
	closed    bool
}

func newInventory(read, gate Policy, sources Sources, cursor func() string) *inventory {
	return &inventory{read: read, gate: gate, sources: sources, cursor: cursor,
		snapshots: map[string]*inventorySnapshot{}, now: time.Now}
}

func (i *inventory) close() {
	i.mu.Lock()
	i.closed = true
	i.snapshots = map[string]*inventorySnapshot{}
	i.mu.Unlock()
}

func (i *inventory) sweep() {
	i.mu.Lock()
	defer i.mu.Unlock()
	for id, s := range i.snapshots {
		if !s.expires.IsZero() && i.now().After(s.expires) {
			delete(i.snapshots, id)
		}
	}
}

// EnableInventory adds abstraction.storage/inventory@1 to this endpoint,
// composed from the designated sources. gate authorizes each call on
// InventoryResource under abstraction.storage/inventory.read; the read policy
// filters every composed record by the digests it carries. Configure it
// before Serve.
func (h *Host) EnableInventory(gate Policy, sources Sources) error {
	h.lifecycle.Lock()
	defer h.lifecycle.Unlock()
	if h.serving || h.ctx.Err() != nil {
		return errInventoryLate
	}
	if gate == nil || sources == nil {
		return errInventoryUnsupported
	}
	h.inventory = newInventory(h.registry.policy, gate, sources, h.changeCursor)
	return nil
}

// InventoryAvailable reports whether the composition is configured and serving.
func (h *Host) InventoryAvailable() bool {
	h.lifecycle.Lock()
	defer h.lifecycle.Unlock()
	return h.inventory != nil && h.ctx.Err() == nil
}

// changeCursor is the content-changes cursor a frozen inventory is taken at,
// empty when this host serves no change journal.
func (h *Host) changeCursor() string {
	h.lifecycle.Lock()
	changes := h.changes
	h.lifecycle.Unlock()
	if changes == nil {
		return ""
	}
	changes.mu.Lock()
	defer changes.mu.Unlock()
	if changes.closed {
		return ""
	}
	return changes.cursorLocked(changes.last)
}

type inventoryReceiver struct {
	inventory *inventory
	scope     string
	peer      *identity.Peer
	ctx       context.Context
}

func inventoryRefusal(outcome api.InventoryOutcome) (api.InventoryPage, error) {
	return api.InventoryPage{Outcome: outcome, Stores: []api.Store{}, Manifests: []api.ManifestHolders{},
		Objects: []api.ObjectHolders{}, Dangling: []api.Dangling{}}, nil
}

func inventoryOutcome(word string) api.InventoryOutcome {
	if outcome, ok := api.ParseInventoryOutcome(word); ok {
		return outcome
	}
	return api.InventoryOutcomeUnavailable
}

func (c inventoryReceiver) authorization() string {
	return decide(c.ctx, c.scope, c.inventory.gate, c.peer, InventoryResource)
}

// readable filters one record: "" includes it, "skip" omits it, and
// "unavailable" refuses the page. A record carrying no digest has nothing to
// decide and is included.
func (c inventoryReceiver) readable(digests []string) string {
	for _, d := range digests {
		switch decide(c.ctx, c.scope, c.inventory.read, c.peer, d) {
		case "":
		case "unavailable":
			return "unavailable"
		default:
			return "skip"
		}
	}
	return ""
}

// compose drains every designated source once and returns the accepted
// records in store then id order. A source that does not answer, or answers
// anything but a page, refuses the whole composition: an inventory that
// silently drops a source it was given would report absence it cannot see.
func (i *inventory) compose(ctx context.Context) ([]api.Store, []record, string) {
	return composeSources(ctx, i.sources(ctx))
}

func composeSources(ctx context.Context, designated []DesignatedSource) ([]api.Store, []record, string) {
	var stores []api.Store
	var records []record
	for _, source := range designated {
		accepted := func(store string) bool {
			for _, s := range source.Stores {
				if s == store {
					return true
				}
			}
			return false
		}
		continuation, first := "", true
		for {
			if ctx.Err() != nil {
				return nil, nil, "unavailable"
			}
			page, err := source.Reader.Snapshot(continuation, MaxInventoryPage)
			if err != nil || page.Outcome != api.SourcePageOutcomePage {
				return nil, nil, "unavailable"
			}
			if first {
				for _, store := range page.Stores {
					if accepted(store.Name) {
						stores = append(stores, store)
					}
				}
				first = false
			}
			for _, m := range page.Manifests {
				if !accepted(m.Manifest.Store) {
					continue
				}
				held := m
				held.Holds = observedHolds(source.Program, m.Holds)
				records = append(records, record{manifest: &held, digests: manifestDigests(m.Manifest)})
			}
			for _, o := range page.Objects {
				if !accepted(o.Store) {
					continue
				}
				held := o
				held.Holds = observedHolds(source.Program, o.Holds)
				records = append(records, record{object: &held, digests: objectDigests(o.Object)})
			}
			for _, d := range page.Dangling {
				want := d
				want.Holder.EstablishedBy = source.Program
				records = append(records, record{dangling: &want})
			}
			if len(records) > MaxInventoryRecords {
				return nil, nil, "unavailable"
			}
			if page.Complete || page.Continuation == "" {
				break
			}
			continuation = page.Continuation
		}
	}
	sort.SliceStable(stores, func(a, b int) bool { return stores[a].Name < stores[b].Name })
	return stores, records, ""
}

// ComposedInventory is one composition taken outside a served page: the
// accepted stores and the records they hold, for a runtime composing them
// into something of its own. It applies the same store acceptance and holder
// rewrite; it applies no read policy, so its caller must not hand it to an
// application.
type ComposedInventory struct {
	Stores    []api.Store
	Manifests []api.ManifestHolders
	Objects   []api.ObjectHolders
	Dangling  []api.Dangling
}

// Compose drains every designated source once. A source that does not answer
// refuses the whole composition, as it does on a served page.
func Compose(ctx context.Context, designated []DesignatedSource) (ComposedInventory, error) {
	stores, records, status := composeSources(ctx, designated)
	if status != "" {
		return ComposedInventory{}, errInventoryUnavailable
	}
	out := ComposedInventory{Stores: stores}
	for _, r := range records {
		switch {
		case r.manifest != nil:
			out.Manifests = append(out.Manifests, *r.manifest)
		case r.object != nil:
			out.Objects = append(out.Objects, *r.object)
		case r.dangling != nil:
			out.Dangling = append(out.Dangling, *r.dangling)
		}
	}
	return out, nil
}

// observedHolds keeps the holds a source may report and writes the bound
// source program into established_by. A hold the source spells as declared,
// or one with no basis, is not an observation and is dropped.
func observedHolds(program string, holds []api.Hold) []api.Hold {
	out := make([]api.Hold, 0, len(holds))
	for _, h := range holds {
		if h.Attestation != api.AttestationObserved || h.Basis == nil {
			continue
		}
		h.Holder.EstablishedBy = program
		out = append(out, h)
	}
	return out
}

func manifestDigests(m api.Manifest) []string {
	var out []string
	for _, e := range m.Entries {
		if e.Evidence != api.EvidenceNone && e.Digest != "" {
			out = append(out, e.Digest)
		}
	}
	return out
}

func objectDigests(o api.Object) []string {
	if o.Evidence == api.EvidenceNone || o.Digest == "" {
		return nil
	}
	return []string{o.Digest}
}

// page fills one page from records starting at offset and reports the index it
// stopped at, or "unavailable" when a read decision was unavailable.
func (c inventoryReceiver) page(records []record, offset int, limit int64) (api.InventoryPage, int, string) {
	out := api.InventoryPage{Outcome: api.InventoryOutcomePage, Stores: []api.Store{}, Manifests: []api.ManifestHolders{},
		Objects: []api.ObjectHolders{}, Dangling: []api.Dangling{}}
	index := offset
	var taken int64
	for ; index < len(records) && taken < limit; index++ {
		r := records[index]
		switch c.readable(r.digests) {
		case "unavailable":
			return api.InventoryPage{}, 0, "unavailable"
		case "skip":
			continue
		}
		switch {
		case r.manifest != nil:
			out.Manifests = append(out.Manifests, *r.manifest)
		case r.object != nil:
			out.Objects = append(out.Objects, *r.object)
		case r.dangling != nil:
			out.Dangling = append(out.Dangling, *r.dangling)
		}
		taken++
	}
	return out, index, ""
}

func (c inventoryReceiver) List(continuation string, limit int64) (api.InventoryPage, error) {
	if limit < 1 || limit > MaxInventoryPage {
		return inventoryRefusal(api.InventoryOutcomeInvalid)
	}
	if status := c.authorization(); status != "" {
		return inventoryRefusal(inventoryOutcome(status))
	}
	i := c.inventory
	i.sweep()
	i.mu.Lock()
	if i.closed {
		i.mu.Unlock()
		return inventoryRefusal(api.InventoryOutcomeUnavailable)
	}
	var id string
	offset := 0
	if continuation == "" {
		if len(i.snapshots) >= MaxSnapshots {
			i.mu.Unlock()
			return inventoryRefusal(api.InventoryOutcomeUnavailable)
		}
		token, err := randomToken()
		if err != nil {
			i.mu.Unlock()
			return inventoryRefusal(api.InventoryOutcomeUnavailable)
		}
		i.mu.Unlock()
		stores, records, status := i.compose(c.ctx)
		if status != "" {
			return inventoryRefusal(inventoryOutcome(status))
		}
		cursor := ""
		if i.cursor != nil {
			cursor = i.cursor()
		}
		i.mu.Lock()
		if i.closed {
			i.mu.Unlock()
			return inventoryRefusal(api.InventoryOutcomeUnavailable)
		}
		id = token
		i.snapshots[id] = &inventorySnapshot{stores: stores, records: records, cursor: cursor, scope: c.scope}
	} else {
		word := ""
		var ok bool
		id, word, ok = strings.Cut(continuation, ":")
		n, err := strconv.Atoi(word)
		if !ok || err != nil || n < 0 || strconv.Itoa(n) != word {
			i.mu.Unlock()
			return inventoryRefusal(api.InventoryOutcomeInvalid)
		}
		offset = n
	}
	snap := i.snapshots[id]
	if snap == nil {
		i.mu.Unlock()
		return inventoryRefusal(api.InventoryOutcomeGap)
	}
	if snap.scope != c.scope {
		i.mu.Unlock()
		return inventoryRefusal(api.InventoryOutcomeForbidden)
	}
	if offset > len(snap.records) {
		i.mu.Unlock()
		return inventoryRefusal(api.InventoryOutcomeInvalid)
	}
	snap.expires = i.now().Add(SnapshotIdle)
	stores, records, cursor := snap.stores, snap.records, snap.cursor
	i.mu.Unlock()

	out, index, status := c.page(records, offset, limit)
	if status != "" {
		return inventoryRefusal(api.InventoryOutcomeUnavailable)
	}
	out.Cursor = cursor
	if offset == 0 {
		out.Stores = stores
	}
	out.Complete = index == len(records)
	if !out.Complete {
		out.Continuation = id + ":" + strconv.Itoa(index)
	}
	if status := c.authorization(); status != "" {
		return inventoryRefusal(inventoryOutcome(status))
	}
	return out, nil
}

// Holders re-reads one target through each source's Verify, as the contract
// says. Nothing is served from the frozen composition: the point of the call
// is an observation made now.
func (c inventoryReceiver) Holders(target string) (api.InventoryPage, error) {
	if target == "" || len(target) > 1024 {
		return inventoryRefusal(api.InventoryOutcomeInvalid)
	}
	if status := c.authorization(); status != "" {
		return inventoryRefusal(inventoryOutcome(status))
	}
	i := c.inventory
	i.mu.Lock()
	closed := i.closed
	i.mu.Unlock()
	if closed {
		return inventoryRefusal(api.InventoryOutcomeUnavailable)
	}
	var records []record
	for _, source := range i.sources(c.ctx) {
		accepted := func(store string) bool {
			for _, s := range source.Stores {
				if s == store {
					return true
				}
			}
			return false
		}
		page, err := source.Reader.Verify(target)
		if err != nil || page.Outcome != api.SourcePageOutcomePage {
			return inventoryRefusal(api.InventoryOutcomeUnavailable)
		}
		for _, m := range page.Manifests {
			if !accepted(m.Manifest.Store) {
				continue
			}
			held := m
			held.Holds = verifiedHolds(source.Program, m.Holds)
			records = append(records, record{manifest: &held, digests: manifestDigests(m.Manifest)})
		}
		for _, o := range page.Objects {
			if !accepted(o.Store) {
				continue
			}
			held := o
			held.Holds = verifiedHolds(source.Program, o.Holds)
			records = append(records, record{object: &held, digests: objectDigests(o.Object)})
		}
		for _, d := range page.Dangling {
			want := d
			want.Holder.EstablishedBy = source.Program
			records = append(records, record{dangling: &want})
		}
		if len(records) > MaxInventoryRecords {
			return inventoryRefusal(api.InventoryOutcomeUnavailable)
		}
	}
	out, index, status := c.page(records, 0, MaxInventoryPage)
	if status != "" {
		return inventoryRefusal(api.InventoryOutcomeUnavailable)
	}
	out.Complete = index == len(records)
	return out, nil
}

// verifiedHolds returns a source's re-read observations as verified holds.
func verifiedHolds(program string, holds []api.Hold) []api.Hold {
	out := observedHolds(program, holds)
	for i := range out {
		out[i].Attestation = api.AttestationVerified
	}
	return out
}

// Unheld needs two observations at least grace_ms apart and a table of
// declared holds. This composition keeps neither, and says so rather than
// reporting content as unheld on one reading.
func (c inventoryReceiver) Unheld(continuation string, limit int64) (api.InventoryPage, error) {
	if limit < 1 || limit > MaxInventoryPage {
		return inventoryRefusal(api.InventoryOutcomeInvalid)
	}
	if status := c.authorization(); status != "" {
		return inventoryRefusal(inventoryOutcome(status))
	}
	return inventoryRefusal(api.InventoryOutcomeUnavailable)
}

// Find answers from one composition taken now, because a digest lookup that
// paged an older snapshot would answer about content that has since moved.
func (c inventoryReceiver) Find(digest string) (api.InventoryPage, error) {
	if !validDigest(digest) {
		return inventoryRefusal(api.InventoryOutcomeInvalid)
	}
	if status := c.authorization(); status != "" {
		return inventoryRefusal(inventoryOutcome(status))
	}
	i := c.inventory
	i.mu.Lock()
	closed := i.closed
	i.mu.Unlock()
	if closed {
		return inventoryRefusal(api.InventoryOutcomeUnavailable)
	}
	_, records, status := i.compose(c.ctx)
	if status != "" {
		return inventoryRefusal(inventoryOutcome(status))
	}
	var matched []record
	for _, r := range records {
		switch {
		case r.dangling != nil:
			if r.dangling.ExpectedDigest == digest {
				matched = append(matched, r)
			}
		default:
			for _, d := range r.digests {
				if d == digest {
					matched = append(matched, r)
					break
				}
			}
		}
	}
	out, index, status := c.page(matched, 0, MaxInventoryPage)
	if status != "" {
		return inventoryRefusal(api.InventoryOutcomeUnavailable)
	}
	out.Complete = index == len(matched)
	return out, nil
}
