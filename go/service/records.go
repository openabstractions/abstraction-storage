package service

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	casapi "github.com/openabstractions/abstraction-cas/go/api"
	api "github.com/openabstractions/abstraction-storage/go/abstraction/storage/content"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// RecordProfile names the service-owned writer identity record format.
const RecordProfile = "abstraction.storage/writer-records@1"

// MaxRecordFileBytes bounds the record file read and replaced through CAS.
const MaxRecordFileBytes = 4 << 20

type recordFile struct {
	Profile string        `json:"profile"`
	Records []recordEntry `json:"records"`
}

// recordEntry persists one request identity. Evidence is present exactly for a
// committed or present result; otherwise the identity is unfinished.
type recordEntry struct {
	Scope      string `json:"scope"`
	Request    string `json:"request"`
	Digest     string `json:"digest"`
	Size       int64  `json:"size"`
	Evidence   string `json:"evidence,omitempty"`
	StoredSize int64  `json:"stored_size,omitempty"`
	Expires    int64  `json:"expires_unix_ms"`
}

// load restores unexpired identities and removes staging left by unfinished
// uploads. Records are written before any staging exists, so each leftover
// staging file belongs to a recorded digest.
func (w *writers) load() error {
	current, err := w.recordStore.Read(w.recordPath)
	if err != nil {
		return fmt.Errorf("storage: read writer records: %w", err)
	}
	w.recordBase = current
	if current.Data == nil {
		return nil
	}
	var file recordFile
	decoder := json.NewDecoder(bytes.NewReader(current.Data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&file); err != nil || decoder.More() || file.Profile != RecordProfile || len(file.Records) > MaxRequestRecords {
		return errors.New("storage: invalid writer record file")
	}
	now := w.now()
	expired := false
	for _, e := range file.Records {
		if e.Scope == "" || strings.Contains(e.Scope, "\x00") || !validRequest(e.Request) || !validDigest(e.Digest) || e.Size < 0 || e.Size > w.limit {
			return errors.New("storage: invalid writer record")
		}
		key := e.Scope + "\x00" + e.Request
		if w.records[key] != nil {
			return errors.New("storage: duplicate writer record")
		}
		rec := &requestRecord{digest: e.Digest, size: e.Size, expires: time.UnixMilli(e.Expires)}
		switch e.Evidence {
		case "":
			w.unreported = errors.Join(w.unreported, w.cleanStaging(e.Digest, e.Size))
		case "hashed", "named":
			if _, found := w.store.Find(e.Digest); found {
				rec.stored = &api.Stored{Digest: e.Digest, Size: e.StoredSize, Evidence: e.Evidence}
			} else {
				// The result was recorded before a publish that never completed.
				w.unreported = errors.Join(w.unreported, w.cleanStaging(e.Digest, e.Size))
				expired = true
			}
		default:
			return errors.New("storage: invalid writer record evidence")
		}
		if !now.Before(rec.expires) {
			expired = true
			continue
		}
		w.records[key] = rec
	}
	if expired {
		return w.saveLocked()
	}
	return nil
}

// cleanStaging removes the provider's reservation for a digest that is not
// findable and returns a failed removal. Place is the provider's
// side-effect-free location choice.
func (w *writers) cleanStaging(digest string, size int64) error {
	if _, found := w.store.Find(digest); found {
		return nil
	}
	ref, err := w.store.Place(digest, size)
	if err != nil || ref.Digest != digest {
		return nil
	}
	path := w.store.Path(ref)
	if !filepath.IsAbs(path) {
		return nil
	}
	if info, err := os.Lstat(path); err == nil && info.Mode().IsRegular() {
		return removeStaged(path)
	}
	return nil
}

// saveLocked replaces the record file from its last observed value. A changed
// file means another owner wrote it; every later save refuses the same way.
// The caller holds w.mu.
func (w *writers) saveLocked() error {
	entries := make([]recordEntry, 0, len(w.records))
	for key, rec := range w.records {
		scope, request, _ := strings.Cut(key, "\x00")
		e := recordEntry{Scope: scope, Request: request, Digest: rec.digest, Size: rec.size, Expires: rec.expires.UnixMilli()}
		if rec.stored != nil {
			e.Evidence, e.StoredSize = rec.stored.Evidence, rec.stored.Size
		}
		entries = append(entries, e)
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Scope != entries[j].Scope {
			return entries[i].Scope < entries[j].Scope
		}
		return entries[i].Request < entries[j].Request
	})
	data, err := json.Marshal(recordFile{Profile: RecordProfile, Records: entries})
	if err != nil {
		return err
	}
	if err := w.recordStore.Write(w.recordPath, w.recordBase, data); err != nil {
		return err
	}
	w.recordBase = casapi.Value{Data: data}
	return nil
}
