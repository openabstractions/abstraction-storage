package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	identity "github.com/openabstractions/abstraction-identity"
	storage "github.com/openabstractions/abstraction-storage/go"
	api "github.com/openabstractions/abstraction-storage/go/abstraction/storage/content"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	// DefaultChangeCapacity is the journal size retained per provider lifetime.
	DefaultChangeCapacity = 4096
	MaxChangePage         = 256
	MaxListedObjects      = 65536
	MaxSnapshots          = 8
	// MaxChangeWaiters leaves connection slots for non-waiting calls.
	MaxChangeWaiters = 16
	SnapshotIdle     = 30 * time.Second
	// ChangesResource is the resource the observe policy receives.
	ChangesResource = "abstraction.storage/changes"
)

type snapshot struct {
	objects []api.ListedObject
	cursor  string
	scope   string
	expires time.Time
}

// changeJournal owns the bounded record of objects a store gained or lost.
type changeJournal struct {
	mu        sync.Mutex
	lister    storage.Lister
	read      Policy
	observe   Policy
	epoch     string
	last      int64
	capacity  int
	ring      []api.Change
	known     map[string]int64
	snapshots map[string]*snapshot
	notify    chan struct{}
	waiters   chan struct{}
	listing   bool
	closed    bool
	now       func() time.Time
}

func randomToken() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

// newChangeJournal records the store's present objects without journaling them.
func newChangeJournal(store storage.Store, read, observe Policy, capacity int) (*changeJournal, error) {
	epoch, err := randomToken()
	if err != nil {
		return nil, err
	}
	j := &changeJournal{read: read, observe: observe, epoch: epoch, capacity: capacity, known: map[string]int64{},
		snapshots: map[string]*snapshot{}, notify: make(chan struct{}), waiters: make(chan struct{}, MaxChangeWaiters), listing: true, now: time.Now}
	if lister, ok := store.(storage.Lister); ok {
		j.lister = lister
		refs, err := lister.List(MaxListedObjects)
		if err != nil {
			return nil, err
		}
		for _, r := range refs {
			j.known[r.Digest] = r.Size
		}
	}
	return j, nil
}

// appendLocked journals one change and wakes waiters. The caller holds j.mu.
func (j *changeJournal) appendLocked(kind, digest string, size int64) {
	j.last++
	j.ring = append(j.ring, api.Change{Sequence: j.last, Kind: kind, Digest: digest, Size: size})
	if len(j.ring) > j.capacity {
		j.ring = append([]api.Change(nil), j.ring[len(j.ring)-j.capacity:]...)
	}
	close(j.notify)
	j.notify = make(chan struct{})
}

// committed journals a service-mediated publish of a new object.
func (j *changeJournal) committed(digest string, size int64) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.closed {
		return
	}
	if _, ok := j.known[digest]; ok {
		return
	}
	j.known[digest] = size
	j.appendLocked(api.ChangeKindAdded, digest, size)
}

// poll diffs the provider listing against known objects. A failed or
// oversized listing makes observation unavailable until a listing succeeds;
// no partial listing is treated as deletions.
func (j *changeJournal) poll() {
	if j.lister == nil {
		return
	}
	refs, err := j.lister.List(MaxListedObjects)
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.closed {
		return
	}
	if err != nil {
		j.listing = false
		return
	}
	j.listing = true
	current := make(map[string]int64, len(refs))
	for _, r := range refs {
		current[r.Digest] = r.Size
	}
	var removed, added []string
	for d := range j.known {
		if _, ok := current[d]; !ok {
			removed = append(removed, d)
		}
	}
	for d := range current {
		if _, ok := j.known[d]; !ok {
			added = append(added, d)
		}
	}
	sort.Strings(removed)
	sort.Strings(added)
	for _, d := range removed {
		size := j.known[d]
		delete(j.known, d)
		j.appendLocked(api.ChangeKindRemoved, d, size)
	}
	for _, d := range added {
		j.known[d] = current[d]
		j.appendLocked(api.ChangeKindAdded, d, current[d])
	}
}

func (j *changeJournal) sweep() {
	j.mu.Lock()
	defer j.mu.Unlock()
	now := j.now()
	for id, s := range j.snapshots {
		if !now.Before(s.expires) {
			delete(j.snapshots, id)
		}
	}
}

func (j *changeJournal) close() {
	j.mu.Lock()
	defer j.mu.Unlock()
	if !j.closed {
		j.closed = true
		close(j.notify)
		j.snapshots = map[string]*snapshot{}
	}
}

func (j *changeJournal) cursorLocked(sequence int64) string {
	return j.epoch + ":" + strconv.FormatInt(sequence, 10)
}

type changesReceiver struct {
	journal *changeJournal
	scope   string
	peer    *identity.Peer
	ctx     context.Context
	wait    context.Context
}

func (c changesReceiver) authorization() string {
	return decide(c.ctx, c.scope, c.journal.observe, c.peer, ChangesResource)
}

// readable filters one digest: "" includes it, "skip" omits it, and
// "unavailable" stops the page without advancing.
func (c changesReceiver) readable(digest string) string {
	switch decide(c.ctx, c.scope, c.journal.read, c.peer, digest) {
	case "":
		return ""
	case "unavailable":
		return "unavailable"
	}
	return "skip"
}

// collect returns a page after the cursor, the journal notification channel
// observed with it, and a refusal outcome.
func (c changesReceiver) collect(cursor string, max int64) (api.ChangePage, <-chan struct{}, string) {
	j := c.journal
	j.mu.Lock()
	if j.closed || !j.listing {
		j.mu.Unlock()
		return api.ChangePage{}, nil, api.ChangePageOutcomeUnavailable
	}
	epoch, word, ok := strings.Cut(cursor, ":")
	sequence, err := strconv.ParseInt(word, 10, 64)
	if !ok || err != nil || sequence < 0 || strconv.FormatInt(sequence, 10) != word {
		j.mu.Unlock()
		return api.ChangePage{}, nil, api.ChangePageOutcomeInvalid
	}
	if epoch != j.epoch {
		j.mu.Unlock()
		return api.ChangePage{}, nil, api.ChangePageOutcomeGap
	}
	if sequence > j.last {
		j.mu.Unlock()
		return api.ChangePage{}, nil, api.ChangePageOutcomeInvalid
	}
	if len(j.ring) > 0 && sequence < j.ring[0].Sequence-1 {
		j.mu.Unlock()
		return api.ChangePage{}, nil, api.ChangePageOutcomeGap
	}
	var candidates []api.Change
	for _, change := range j.ring {
		if change.Sequence > sequence {
			candidates = append(candidates, change)
		}
	}
	notify := j.notify
	j.mu.Unlock()

	page := api.ChangePage{Outcome: api.ChangePageOutcomePage, Changes: []api.Change{}, AtEnd: true}
	advanced := sequence
	for i, change := range candidates {
		if int64(len(page.Changes)) == max {
			page.AtEnd = false
			break
		}
		switch c.readable(change.Digest) {
		case "unavailable":
			return api.ChangePage{}, nil, api.ChangePageOutcomeUnavailable
		case "":
			page.Changes = append(page.Changes, change)
		}
		advanced = change.Sequence
		if i == len(candidates)-1 {
			page.AtEnd = true
		}
	}
	page.Next = epoch + ":" + strconv.FormatInt(advanced, 10)
	return page, notify, ""
}

func (c changesReceiver) Observe(cursor string, maxChanges, waitMS int64) (api.ChangePage, error) {
	refusal := func(outcome string) (api.ChangePage, error) {
		return api.ChangePage{Outcome: outcome, Changes: []api.Change{}, Next: cursor}, nil
	}
	if len(cursor) > 256 || maxChanges < 1 || maxChanges > MaxChangePage || waitMS < 0 || waitMS > 30000 {
		return refusal(api.ChangePageOutcomeInvalid)
	}
	if status := c.authorization(); status != "" {
		return refusal(status)
	}
	start := cursor
	if start == "" {
		j := c.journal
		j.mu.Lock()
		start = j.cursorLocked(j.last)
		j.mu.Unlock()
	}
	page, notify, status := c.collect(start, maxChanges)
	if status != "" {
		return refusal(status)
	}
	if len(page.Changes) == 0 && page.AtEnd && waitMS > 0 {
		select {
		case c.journal.waiters <- struct{}{}:
		default:
			return refusal(api.ChangePageOutcomeUnavailable)
		}
		timer := time.NewTimer(time.Duration(waitMS) * time.Millisecond)
		select {
		case <-notify:
		case <-timer.C:
		case <-c.wait.Done():
		}
		timer.Stop()
		<-c.journal.waiters
		if c.wait.Err() != nil {
			return refusal(api.ChangePageOutcomeUnavailable)
		}
		if page, _, status = c.collect(page.Next, maxChanges); status != "" {
			return refusal(status)
		}
	}
	// Recheck observe authorization before any change is returned.
	if status := c.authorization(); status != "" {
		return refusal(status)
	}
	return page, nil
}

func (c changesReceiver) List(continuation string, limit int64) (api.ListingPage, error) {
	refusal := func(outcome string) (api.ListingPage, error) {
		return api.ListingPage{Outcome: outcome, Objects: []api.ListedObject{}}, nil
	}
	if len(continuation) > 256 || limit < 1 || limit > MaxChangePage {
		return refusal(api.ListingOutcomeInvalid)
	}
	if status := c.authorization(); status != "" {
		return refusal(status)
	}
	j := c.journal
	j.sweep()
	j.mu.Lock()
	if j.closed || !j.listing {
		j.mu.Unlock()
		return refusal(api.ListingOutcomeUnavailable)
	}
	var id string
	offset := 0
	if continuation == "" {
		if len(j.snapshots) >= MaxSnapshots {
			j.mu.Unlock()
			return refusal(api.ListingOutcomeUnavailable)
		}
		token, err := randomToken()
		if err != nil {
			j.mu.Unlock()
			return refusal(api.ListingOutcomeUnavailable)
		}
		objects := make([]api.ListedObject, 0, len(j.known))
		for d, size := range j.known {
			objects = append(objects, api.ListedObject{Digest: d, Size: size})
		}
		sort.Slice(objects, func(a, b int) bool { return objects[a].Digest < objects[b].Digest })
		id = token
		j.snapshots[id] = &snapshot{objects: objects, cursor: j.cursorLocked(j.last), scope: c.scope}
	} else {
		word := ""
		var ok bool
		id, word, ok = strings.Cut(continuation, ":")
		n, err := strconv.Atoi(word)
		if !ok || err != nil || n < 0 || strconv.Itoa(n) != word {
			j.mu.Unlock()
			return refusal(api.ListingOutcomeInvalid)
		}
		offset = n
	}
	snap := j.snapshots[id]
	if snap == nil {
		j.mu.Unlock()
		return refusal(api.ListingOutcomeGap)
	}
	if snap.scope != c.scope {
		j.mu.Unlock()
		return refusal(api.ListingOutcomeForbidden)
	}
	if offset > len(snap.objects) {
		j.mu.Unlock()
		return refusal(api.ListingOutcomeInvalid)
	}
	snap.expires = j.now().Add(SnapshotIdle)
	objects, cursor := snap.objects, snap.cursor
	j.mu.Unlock()

	page := api.ListingPage{Outcome: api.ListingOutcomePage, Objects: []api.ListedObject{}, Cursor: cursor}
	index := offset
	for ; index < len(objects) && int64(len(page.Objects)) < limit; index++ {
		switch c.readable(objects[index].Digest) {
		case "unavailable":
			return refusal(api.ListingOutcomeUnavailable)
		case "":
			page.Objects = append(page.Objects, objects[index])
		}
	}
	page.Complete = index == len(objects)
	if !page.Complete {
		page.Continuation = id + ":" + strconv.Itoa(index)
	}
	if status := c.authorization(); status != "" {
		return refusal(status)
	}
	return page, nil
}

// errChangesUnsupported is reported when observation cannot be enabled.
var errChangesUnsupported = errors.New("storage: change observation requires an explicit observe policy and positive capacity")
