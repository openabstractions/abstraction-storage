package service

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"strings"

	identity "github.com/openabstractions/abstraction-identity"
	"github.com/openabstractions/abstraction-identity/listen"
	storage "github.com/openabstractions/abstraction-storage/go"
	api "github.com/openabstractions/abstraction-storage/go/abstraction/storage/content"
)

// This private Panel protocol shares the bound Storage endpoint. It can
// select a retained removal occurrence and carries no file or sink locator.
const archiveRestoreFrame = "OA/storage-archive-restore@1\n"

type ArchiveRestoreEntry struct {
	Occurrence string `json:"occurrence"`
	Digest     string `json:"digest"`
	Bytes      int64  `json:"bytes"`
	At         int64  `json:"at"`
	Available  bool   `json:"available"`
	Status     string `json:"status,omitempty"`
}

type ArchiveRestoreReply struct {
	Outcome string                `json:"outcome"`
	Entries []ArchiveRestoreEntry `json:"entries,omitempty"`
}

// ArchiveActor is the durable projection captured at the receiving boundary.
type ArchiveActor struct {
	AccountKind string
	Account     string
	Program     string
	Executable  string
}

type ArchivePlacement func(ctx context.Context, actor ArchiveActor, id, digest string, size int64, body io.Reader, check func(context.Context, WriteCall) error) error
type ArchiveRelease func(actor ArchiveActor, id, digest string, size int64) error
type ArchiveRestoreHandler func(ctx context.Context, peer *identity.Peer, actor ArchiveActor, occurrence string) ArchiveRestoreReply

func ArchiveRestore(ctx context.Context, transport listen.FrameClient, occurrence string) (ArchiveRestoreReply, error) {
	raw, err := json.Marshal(occurrence)
	if err != nil {
		return ArchiveRestoreReply{}, err
	}
	reply, err := transport.ExchangeFrameContext(ctx, append([]byte(archiveRestoreFrame), raw...))
	if err != nil {
		return ArchiveRestoreReply{}, err
	}
	var result ArchiveRestoreReply
	err = json.Unmarshal(reply, &result)
	return result, err
}

func (h *Component) archiveRestore(call *listen.FramedCall, ctx context.Context) ([]byte, bool) {
	payload, ok := bytes.CutPrefix(call.Frame, []byte(archiveRestoreFrame))
	if !ok {
		return nil, false
	}
	out := ArchiveRestoreReply{Outcome: "forbidden"}
	var occurrence string
	if len(payload) > 128 || json.Unmarshal(payload, &occurrence) != nil || occurrence != "" && !validArchiveRestoreSelection(occurrence) {
		return archiveReply(ArchiveRestoreReply{Outcome: "invalid"}), true
	}
	peer, err := call.Peer()
	if err != nil {
		return archiveReply(out), true
	}
	h.lifecycle.Lock()
	writers, removals := h.writers, h.removals
	h.lifecycle.Unlock()
	if writers == nil || removals == nil || removals.options.ArchiveRestore == nil {
		return archiveReply(ArchiveRestoreReply{Outcome: "unsupported"}), true
	}
	binding := receivingNativeBinding(peer, h.owner)
	if binding == nil {
		return archiveReply(out), true
	}
	return archiveReply(removals.options.ArchiveRestore(ctx, peer, ArchiveActor(binding.owner), occurrence)), true
}

func validArchiveRestoreSelection(value string) bool {
	occurrence, proof, ok := strings.Cut(value, ".")
	canonicalHex := func(value string, n int) bool {
		raw, err := hex.DecodeString(value)
		return err == nil && len(raw) == n && value == hex.EncodeToString(raw)
	}
	return ok && canonicalHex(occurrence, 16) && canonicalHex(proof, 32)
}

func archiveReply(result ArchiveRestoreReply) []byte {
	raw, _ := json.Marshal(result)
	return raw
}

const archiveWritePrefix = "archive-restore-"

func validArchiveWriteRequest(request string) bool {
	id, ok := strings.CutPrefix(request, archiveWritePrefix)
	raw, err := hex.DecodeString(id)
	return ok && err == nil && len(raw) == 16 && hex.EncodeToString(raw) == id
}

func (w *writers) placeArchive(ctx context.Context, actor ArchiveActor, id, digest string, size int64, body io.Reader, check func(context.Context, WriteCall) error) error {
	native := nativeOwner(actor)
	request := archiveWritePrefix + id
	if !native.valid(native.scope()) || !validArchiveWriteRequest(request) || size <= 0 || check == nil {
		return errors.New("storage: invalid archive placement admission")
	}
	if body == nil {
		return w.proveArchiveReceipt(ctx, native, request, digest, size, true)
	}
	ctx = context.WithValue(ctx, nativeBindingKey{}, &nativeBinding{owner: native})
	ctx = context.WithValue(ctx, archiveWriteKey{}, &archiveWrite{request, digest, size, check})
	c := writeReceiver{writers: w, scope: native.scope(), ctx: ctx}
	start, err := c.Begin(request, digest, size)
	if err != nil {
		return err
	}
	if start.Outcome == api.BeginOutcomeCommitted {
		return w.proveArchiveReceipt(ctx, native, request, digest, size, true)
	}
	if start.Outcome != api.BeginOutcomeStarted || start.Upload == nil {
		return errors.New("storage: archive placement refused")
	}
	handle := start.Upload.Handle
	defer c.Abort(handle)
	buffer := make([]byte, min(int(api.MaxAppendBytes), 256<<10))
	for offset := int64(0); offset < size; {
		n, err := io.ReadFull(body, buffer[:min(int64(len(buffer)), size-offset)])
		if err != nil {
			return err
		}
		appended, err := c.Append(handle, offset, buffer[:n])
		if err != nil || appended.Outcome != api.AppendOutcomeAccepted || appended.Received != offset+int64(n) {
			return errors.Join(err, errors.New("storage: archive write refused"))
		}
		offset = appended.Received
	}
	// The writer rehashes the full staged object and checks ordinary
	// publication rights under its existing reservation before Commit.
	committed, err := c.Commit(handle)
	if err != nil || committed.Outcome != api.CommitOutcomeCommitted {
		return errors.Join(err, errors.New("storage: archive commit refused"))
	}
	return w.proveArchiveReceipt(ctx, native, request, digest, size, false)
}

func (w *writers) proveArchiveReceipt(ctx context.Context, native nativeOwner, request, digest string, size int64, delayed bool) error {
	w.mu.Lock()
	record := w.records[native.scope()+"\x00"+request]
	committed := !w.closed && w.currentLocked() && record != nil && record.heldArchiveReceipt(request) &&
		record.native.sameProgram(native) && record.digest == digest && record.size == size &&
		record.handle == "" && record.stored.Size == size
	var key string
	if committed {
		key = record.publishedFile
	}
	w.mu.Unlock()
	if store, ok := w.store.(*storage.ContentStore); committed && ok && store.VerifyArchivePublication(ctx, digest, size, key, delayed) {
		return nil
	}
	return errors.New("storage: no retained archive placement")
}

// releaseArchive runs only after the durable Download job has settled.
func (w *writers) releaseArchive(actor ArchiveActor, id, digest string, size int64) error {
	native := nativeOwner(actor)
	request := archiveWritePrefix + id
	if !native.valid(native.scope()) || !validArchiveWriteRequest(request) {
		return errors.New("storage: invalid archive receipt release")
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	key := native.scope() + "\x00" + request
	rec := w.records[key]
	if rec == nil {
		return nil
	}
	if w.closed || !w.currentLocked() || rec.native == nil || !rec.native.sameProgram(native) ||
		rec.digest != digest || rec.size != size || rec.handle != "" {
		return errors.New("storage: archive receipt release refused")
	}
	delete(w.records, key)
	if err := w.saveLocked(); err != nil {
		w.records[key] = rec
		return err
	}
	return nil
}
