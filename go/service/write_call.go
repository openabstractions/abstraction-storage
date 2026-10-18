package service

import (
	"context"
	"errors"
)

// WriteStage names the writer call a write Policy decides (STO-W11).
type WriteStage int

const (
	// WriteBegin is Begin, before any lookup or staging effect.
	WriteBegin WriteStage = iota + 1
	// WriteAppend is Append, before bytes are staged.
	WriteAppend
	// WriteCommit is Commit, before the staged bytes are verified.
	WriteCommit
	// WritePublish is Commit after every declared byte matched the digest and
	// before the result is recorded or published. A refusal here publishes
	// nothing and leaves the upload to its idle expiry.
	WritePublish
)

// HeadBytes bounds WriteCall.Head: enough for every media signature the
// runtime classifies request input by.
const HeadBytes = 512

// ErrRequireBytes, returned by a write Policy at WriteBegin, permits the
// upload on condition that the writer proves possession of the bytes
// (STO-W6): Begin answers no present for an object already stored, and the
// upload stages and verifies every byte. Publication then reports the object
// as existing and keeps the stored bytes. At any other stage it refuses.
var ErrRequireBytes = errors.New("storage: write permitted on proof of the bytes")

// WriteCall describes the writer call a write Policy is deciding. The writer
// attaches it to the context it passes the policy; a policy that ignores it
// decides by digest alone, as before STO-W11.
type WriteCall struct {
	Stage WriteStage
	// Size is the declared size of the upload.
	Size int64
	// Head holds the first HeadBytes verified bytes at WritePublish, nil
	// otherwise. The policy must not retain it.
	Head []byte
	// Existing is set at WritePublish when the store already held the object
	// before this upload published: the write proved the bytes and publishes
	// nothing new.
	Existing bool
}

type writeCallKey struct{}

// WithWriteCall returns ctx carrying call, as the writer passes it to a write
// Policy. Compositions and tests use it to decide a call outside the writer.
func WithWriteCall(ctx context.Context, call WriteCall) context.Context {
	return context.WithValue(ctx, writeCallKey{}, call)
}

// WriteCallFrom returns the writer call ctx describes, if any.
func WriteCallFrom(ctx context.Context) (WriteCall, bool) {
	call, ok := ctx.Value(writeCallKey{}).(WriteCall)
	return call, ok
}
