package client

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/openabstractions/abstraction-identity/listen"
	api "github.com/openabstractions/abstraction-storage/go/abstraction/storage/content"
	"io"
	"time"
)

type Upload = api.Upload
type Stored = api.Stored

// MaxAppendBytes bounds one Append payload.
const MaxAppendBytes = 65536

// Writer stages bounded uploads through one content-writer binding.
type Writer struct{ transport listen.FrameClient }

// OutcomeError reports a typed service outcome that ended a Write.
type OutcomeError struct{ Operation, Outcome string }

func (e *OutcomeError) Error() string {
	return fmt.Sprintf("storage: %s %s", e.Operation, e.Outcome)
}

func NewWriter(endpoint string) *Writer {
	return NewWriterWithTransport(listen.FrameClient{Endpoint: endpoint})
}

// NewWriterWithTransport retains the caller's endpoint, server trust and waiting limits.
func NewWriterWithTransport(transport listen.FrameClient) *Writer {
	return &Writer{transport.WithDefaults(5*time.Second, 1<<20)}
}

// NewRequestID creates a request identity. Retain it before Begin to reconcile
// a lost reply; the service scopes it to the calling account and program.
func NewRequestID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

func validRequest(r string) bool {
	if len(r) < 16 || len(r) > 128 {
		return false
	}
	for _, c := range r {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-') {
			return false
		}
	}
	return true
}

func validUpload(u Upload) bool {
	return u.Handle != "" && len(u.Handle) <= 128 && validDigest(u.Digest) && u.Size >= 0 && u.Received >= 0 && u.Received <= u.Size
}

func (w *Writer) Begin(ctx context.Context, request, digest string, size int64) (api.BeginResult, error) {
	if !validRequest(request) || !validDigest(digest) || size < 0 {
		return api.BeginResult{}, errors.New("storage: invalid write request")
	}
	if e := ctx.Err(); e != nil {
		return api.BeginResult{}, e
	}
	r, e := api.NewContentWriterClient(w.transport.WithContext(ctx)).Begin(request, digest, size)
	if e != nil {
		return api.BeginResult{}, e
	}
	storedOutcome := r.Outcome == "committed" || r.Outcome == "present"
	switch {
	case (r.Outcome == "started") != (r.Upload != nil), storedOutcome != (r.Stored != nil), r.Limit < 0:
		return api.BeginResult{}, errors.New("storage: inconsistent begin result")
	case r.Upload != nil && (!validUpload(*r.Upload) || r.Upload.Digest != digest || r.Upload.Size != size):
		return api.BeginResult{}, errors.New("storage: inconsistent upload")
	case r.Stored != nil && (r.Stored.Digest != digest || (r.Outcome == "committed") != (r.Stored.Evidence == "hashed") || r.Outcome == "committed" && r.Stored.Size != size):
		return api.BeginResult{}, errors.New("storage: inconsistent stored result")
	}
	return r, nil
}

func (w *Writer) Append(ctx context.Context, upload Upload, offset int64, data []byte) (api.AppendResult, error) {
	n := int64(len(data))
	if !validUpload(upload) || offset < 0 || n < 1 || n > MaxAppendBytes {
		return api.AppendResult{}, errors.New("storage: invalid append bounds")
	}
	if e := ctx.Err(); e != nil {
		return api.AppendResult{}, e
	}
	r, e := api.NewContentWriterClient(w.transport.WithContext(ctx)).Append(upload.Handle, offset, data)
	if e != nil {
		return api.AppendResult{}, e
	}
	switch r.Outcome {
	case "accepted":
		if r.Received != offset+n || r.Received > upload.Size {
			return api.AppendResult{}, errors.New("storage: inconsistent append result")
		}
	case "out_of_order", "too_large":
		if r.Received < 0 || r.Received > upload.Size {
			return api.AppendResult{}, errors.New("storage: inconsistent append result")
		}
	default:
		if r.Received != 0 {
			return api.AppendResult{}, errors.New("storage: inconsistent append result")
		}
	}
	return r, nil
}

func (w *Writer) Commit(ctx context.Context, upload Upload) (api.CommitResult, error) {
	if !validUpload(upload) {
		return api.CommitResult{}, errors.New("storage: invalid upload")
	}
	if e := ctx.Err(); e != nil {
		return api.CommitResult{}, e
	}
	r, e := api.NewContentWriterClient(w.transport.WithContext(ctx)).Commit(upload.Handle)
	if e != nil {
		return api.CommitResult{}, e
	}
	if (r.Outcome == "committed") != (r.Stored != nil) || r.Stored != nil && (r.Stored.Digest != upload.Digest || r.Stored.Size != upload.Size || r.Stored.Evidence != "hashed") ||
		r.Outcome != "incomplete" && r.Received != 0 || r.Received < 0 || r.Received > upload.Size {
		return api.CommitResult{}, errors.New("storage: inconsistent commit result")
	}
	return r, nil
}

func (w *Writer) Abort(ctx context.Context, upload Upload) (api.AbortResult, error) {
	if !validUpload(upload) {
		return api.AbortResult{}, errors.New("storage: invalid upload")
	}
	if e := ctx.Err(); e != nil {
		return api.AbortResult{}, e
	}
	return api.NewContentWriterClient(w.transport.WithContext(ctx)).Abort(upload.Handle)
}

// Write uploads size bytes from content under a caller-retained request
// identity. Retrying Write with the same identity after an uncertain failure
// resumes the live upload or returns its committed result. It never aborts.
func (w *Writer) Write(ctx context.Context, request, digest string, content io.ReaderAt, size int64) (Stored, error) {
	begun, err := w.Begin(ctx, request, digest, size)
	if err != nil {
		return Stored{}, err
	}
	switch begun.Outcome {
	case "committed", "present":
		return *begun.Stored, nil
	case "started":
	default:
		return Stored{}, &OutcomeError{"begin", begun.Outcome}
	}
	upload := *begun.Upload
	buffer := make([]byte, MaxAppendBytes)
	for offset := upload.Received; offset < size; {
		n := min(int64(MaxAppendBytes), size-offset)
		read, err := content.ReadAt(buffer[:n], offset)
		if int64(read) != n {
			if err == nil {
				err = io.ErrUnexpectedEOF
			}
			return Stored{}, err
		}
		appended, err := w.Append(ctx, upload, offset, buffer[:n])
		if err != nil {
			return Stored{}, err
		}
		switch appended.Outcome {
		case "accepted", "out_of_order":
			offset = appended.Received
		default:
			return Stored{}, &OutcomeError{"append", appended.Outcome}
		}
	}
	committed, err := w.Commit(ctx, upload)
	if err != nil {
		return Stored{}, err
	}
	if committed.Outcome != "committed" {
		return Stored{}, &OutcomeError{"commit", committed.Outcome}
	}
	return *committed.Stored, nil
}
