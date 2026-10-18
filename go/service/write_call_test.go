package service

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	casapi "github.com/openabstractions/abstraction-cas/go/api"
	identity "github.com/openabstractions/abstraction-identity"
	storage "github.com/openabstractions/abstraction-storage/go"
)

// STO-W6: a policy that permits Begin only on proof of the bytes gets no
// present answer: Begin answers started whether or not the object exists,
// the upload stages and verifies every byte, and publication reports the
// object as existing and keeps the stored bytes.
func TestWriterRequiringBytesAnswersNoPresent(t *testing.T) {
	root := t.TempDir()
	content, err := storage.NewContentStore("fixture", root)
	if err != nil {
		t.Fatal(err)
	}
	stored := []byte("\x89PNG\r\n\x1a\nstored by another writer")
	digest := named(stored)
	ref, err := content.Place(digest, int64(len(stored)))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(content.Path(ref), stored, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := content.Commit(ref); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var published []WriteCall
	policy := func(ctx context.Context, _ *identity.Peer, _ string) error {
		call, _ := WriteCallFrom(ctx)
		switch call.Stage {
		case WriteBegin:
			return ErrRequireBytes
		case WritePublish:
			mu.Lock()
			published = append(published, call)
			mu.Unlock()
		}
		return nil
	}
	records := filepath.Join(t.TempDir(), "records.json")
	w, err := newWriters(content, policy, 1<<20, casapi.BoundedFileStore{MaxBytes: MaxRecordFileBytes}, records, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(w.close)
	added := 0
	w.onCommitted = func(string, int64) { added++ }
	c := writeReceiver{w, "second-program", nil, context.Background()}
	absent := named([]byte("never stored"))
	for _, d := range []string{digest, absent} {
		if begun, _ := c.Begin(strings.Repeat("o", 20)+d[7:17], d, int64(len(stored))); begun.Outcome.String() != "started" {
			t.Fatalf("Begin on proof of bytes answered %v for %s", begun.Outcome, d)
		}
	}
	// The absent digest's upload is aborted; the stored one is proved.
	begun, _ := c.Begin(strings.Repeat("o", 20)+digest[7:17], digest, int64(len(stored)))
	wrong := bytes.Repeat([]byte("x"), len(stored))
	if r, _ := c.Append(begun.Upload.Handle, 0, wrong); r.Outcome.String() != "accepted" {
		t.Fatal(r)
	}
	if r, _ := c.Commit(begun.Upload.Handle); r.Outcome.String() != "mismatch" {
		t.Fatalf("other bytes claimed a stored object: %v", r.Outcome)
	}
	begun, _ = c.Begin(strings.Repeat("q", 20)+digest[7:17], digest, int64(len(stored)))
	if begun.Outcome.String() != "started" {
		t.Fatal(begun)
	}
	if r, _ := c.Append(begun.Upload.Handle, 0, stored); r.Outcome.String() != "accepted" {
		t.Fatal(r)
	}
	committed, _ := c.Commit(begun.Upload.Handle)
	if committed.Outcome.String() != "committed" || committed.Stored.Evidence.String() != "hashed" {
		t.Fatalf("proved bytes: %+v", committed)
	}
	mu.Lock()
	last := published[len(published)-1]
	mu.Unlock()
	if !last.Existing {
		t.Fatal("publication of proved bytes did not report the object as existing")
	}
	if added != 0 {
		t.Fatal("proved bytes journaled a new object")
	}
	blob, ok := content.Find(digest)
	if !ok {
		t.Fatal("the stored object is gone")
	}
	if got, err := os.ReadFile(content.Path(blob)); err != nil || !bytes.Equal(got, stored) {
		t.Fatalf("stored bytes changed: %v", err)
	}
	if leftovers, _ := filepath.Glob(filepath.Join(filepath.Dir(records), possessionPattern)); len(leftovers) != 0 {
		t.Fatalf("possession files left behind: %v", leftovers)
	}
}

// STO-W11: the writer names each call it asks the write policy about, and
// decides the verified bytes once more before recording or publishing them.
func TestWriterPolicySeesEachStageAndTheVerifiedHead(t *testing.T) {
	content, err := storage.NewContentStore("fixture", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var calls []WriteCall
	refusePublish := false
	policy := func(ctx context.Context, _ *identity.Peer, _ string) error {
		call, ok := WriteCallFrom(ctx)
		if !ok {
			return errors.New("writer call missing")
		}
		mu.Lock()
		defer mu.Unlock()
		calls = append(calls, call)
		if call.Stage == WritePublish && refusePublish {
			return errors.New("publish refused")
		}
		return nil
	}
	w, err := newWriters(content, policy, 1<<20, casapi.BoundedFileStore{MaxBytes: MaxRecordFileBytes}, filepath.Join(t.TempDir(), "records.json"), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(w.close)
	c := writeReceiver{w, "account-program", nil, context.Background()}
	body := bytes.Repeat([]byte("0123456789"), 100) // 1000 bytes, two appends
	digest := named(body)

	refusePublish = true
	begun, _ := c.Begin(strings.Repeat("s", 32), digest, int64(len(body)))
	if begun.Outcome.String() != "started" {
		t.Fatal(begun)
	}
	for _, part := range [][2]int{{0, 300}, {300, len(body)}} {
		if r, _ := c.Append(begun.Upload.Handle, int64(part[0]), body[part[0]:part[1]]); r.Outcome.String() != "accepted" {
			t.Fatal(r)
		}
	}
	if r, _ := c.Commit(begun.Upload.Handle); r.Outcome.String() != "forbidden" {
		t.Fatalf("refused publish answered %v", r.Outcome)
	}
	if _, ok := content.Find(digest); ok {
		t.Fatal("a publish the policy refused became findable")
	}
	mu.Lock()
	stages := make([]WriteStage, 0, len(calls))
	for _, call := range calls {
		stages = append(stages, call.Stage)
		if call.Size != int64(len(body)) {
			t.Fatalf("stage %d declared size %d", call.Stage, call.Size)
		}
	}
	last := calls[len(calls)-1]
	refusePublish = false
	calls = nil
	mu.Unlock()
	want := []WriteStage{WriteBegin, WriteAppend, WriteAppend, WriteCommit, WritePublish}
	if len(stages) != len(want) {
		t.Fatalf("stages %v, want %v", stages, want)
	}
	for i := range want {
		if stages[i] != want[i] {
			t.Fatalf("stages %v, want %v", stages, want)
		}
	}
	if !bytes.Equal(last.Head, body[:HeadBytes]) {
		t.Fatalf("publish head is %d bytes, want the first %d verified bytes", len(last.Head), HeadBytes)
	}

	// The refused upload stays live; a permitted retry publishes it.
	if r, _ := c.Commit(begun.Upload.Handle); r.Outcome.String() != "committed" {
		t.Fatalf("permitted retry answered %v", r.Outcome)
	}
	if _, ok := content.Find(digest); !ok {
		t.Fatal("permitted publish is not findable")
	}
}
