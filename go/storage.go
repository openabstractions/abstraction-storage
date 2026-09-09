// Package storage is where bytes live, addressed by what they are rather than
// by what somebody decided to call them.
//
// # The problem, measured rather than assumed
//
// One ordinary machine, four tools, four stores that know nothing about each
// other:
//
//	LM Studio          44 GB   names files by filename only   NOT dedupable
//	Lemonade           26 GB   content hash
//	Ollama             23 GB   content hash
//	HuggingFace cache  23 GB   content hash
//
// 116 GB, and the same weights sit in more than one of them under different
// names. Not because the problem is hard — because there is no shared interface,
// so each tool solves it privately and none of them solve it well. This project
// opened by measuring that and then spent months not building the layer.
//
// # What an interface here has to be able to say
//
// Two things, and the second is the one that was missing:
//
//	Find   are these exact bytes already on this machine?
//	Place  where should bytes we do not have yet go?
//
// Find is what makes dedup possible: once a registry has told us an artifact's
// digest, discovering that Ollama already has it is a stat, not a download. That
// half already existed, in the model layer, reading three real stores.
//
// Place is what stops an application inventing a path. Before it,
// download.Sink{Partial, Final} were two strings every caller filled in by
// hand — so the layer that is pluggable about WHO fetches the bytes was
// hardcoded about WHERE they land, which is the same defect at the other end of
// one call.
//
// # What this must not become
//
// Not a filesystem. A Ref is opaque, and a store that happens to be a directory
// says so through the Local capability rather than by returning paths from the
// interface. That is the same shape job.Scratch has, for the same reason: the
// binding must not be able to name itself upward.
package storage

import "errors"

// Ref is a handle to bytes in a store. Opaque on purpose.
//
// The application that asked for a download does not learn a path from it, and
// could not act on one if it did — the process that finally writes those bytes
// may be a Windows service under its own account, or a daemon on a NAS that
// mounts this store somewhere else entirely.
type Ref struct {
	// Store names which store this came from, so a caller holding several refs
	// can say where each one is without asking.
	Store string
	// Digest is what the bytes are, "sha256:<hex>", when it is known. A store
	// may hold bytes whose digest nobody has computed.
	Digest string
	// Size is how big, or 0 when unknown.
	Size int64

	// locator is how the store's own binding finds it again. Unexported: a
	// caller that could read it would start joining paths onto it.
	locator string
}

// Store is somewhere bytes live.
//
// Deliberately small. Everything a store must be able to do is here; everything
// only SOME stores can do is a capability below.
type Store interface {
	// Name is how this store is identified to a person. "ollama", "nas".
	Name() string

	// Find reports where these bytes already are. The digest is authoritative;
	// a store that cannot answer by digest cannot participate, and LM Studio —
	// the largest store on the machine that was measured — is exactly that case.
	//
	// It must not hash anything. A hit is trusted only as far as the store's own
	// naming convention goes, which is why whoever copies from it still
	// verifies: Ollama's blobs hash to their own filenames, and Ollama does not
	// check them, so a corrupt blob there is a real possibility.
	Find(digest string) (Ref, bool)

	// Place reserves somewhere for bytes this store does not have yet.
	//
	// Reserving is not writing. The caller may be handing this to a service that
	// will do the writing much later, under a different account, after this
	// process has exited — so Place settles WHERE and nothing else.
	Place(digest string, size int64) (Ref, error)
}

// Local is an OPTIONAL capability: a store whose binding is a filesystem can
// name a location on it.
//
// It exists because bytes are not always moved by us. BITS writes under its own
// service account and hands the file over on completion; a NAS daemon writes on
// the far side of a share. Neither can be given an io.Writer, so a destination
// they can act on has to be expressible — and that destination is a path.
//
// It is a capability rather than part of Store for the same reason job.Scratch
// is: a store backed by a service answers no, and a caller then has to have a
// real answer for that case instead of assuming a directory exists.
type Local interface {
	// Path is where this ref is on this machine's filesystem.
	Path(Ref) string
}

// Writable is an OPTIONAL capability: a store that can take bytes directly,
// for a caller that is holding them rather than delegating the transfer.
type Writable interface {
	// Commit moves already-written bytes into the store under their digest,
	// making them findable. Until this is called, a placed ref is a reservation
	// and nothing more — which is the same two-phase shape the job layer uses
	// for TRANSFERRED, and for the same reason: the writer and the consumer are
	// not the same process.
	Commit(Ref) error
}

var (
	// ErrReadOnly is returned by Place on a store that can only be read. The
	// foreign stores are all like this: we look inside Ollama's blobs, and we
	// do not write there, because their layout is theirs to change.
	ErrReadOnly = errors.New("storage: this store is read-only")
	// ErrNotFound means these bytes are not here.
	ErrNotFound = errors.New("storage: not found")
)

// Locator returns a ref's internal location. For the store that produced it.
//
// Exported for bindings, not for applications: it is how a Local implementation
// turns a ref back into something it can act on. An application that reaches for
// this is doing the thing this package exists to stop.
func Locator(r Ref) string { return r.locator }

// NewRef is how a Store constructs a ref. Also for bindings.
func NewRef(store, digest, locator string, size int64) Ref {
	return Ref{Store: store, Digest: digest, Size: size, locator: locator}
}
