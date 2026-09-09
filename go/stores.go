package storage

import "fmt"

// Stores searches several in order, which is the whole point of having an
// interface rather than one store.
//
// Order matters and is the caller's: Find asks each in turn and returns the
// first hit, so a machine that lists its own store first prefers bytes it
// verified over bytes some other tool wrote. Place asks the first store that
// can take them, so read-only foreign stores are skipped without a special case.
type Stores struct{ all []Store }

func New(s ...Store) *Stores { return &Stores{all: s} }

func (s *Stores) Add(x Store) { s.all = append(s.all, x) }

// Names lists the distinct stores, in order.
//
// Distinct because the HuggingFace cache keeps one blobs directory per repo, so
// a machine with forty models has forty huggingface stores — true, and useless
// in a diagnostic line. What a person wants to know is which TOOLS are holding
// bytes on this machine.
func (s *Stores) Names() []string {
	out := make([]string, 0, len(s.all))
	seen := map[string]bool{}
	for _, x := range s.all {
		if seen[x.Name()] {
			continue
		}
		seen[x.Name()] = true
		out = append(out, x.Name())
	}
	return out
}

// Len is how many stores are actually being searched, which is the number that
// matters for whether a lookup is cheap.
func (s *Stores) Len() int { return len(s.all) }

func (s *Stores) Name() string { return "stores" }

// Find returns the first copy, and FindAll returns every one. Both exist
// because they answer different questions: "can I avoid downloading this" wants
// the first, and "how many times is this on my disk" wants all of them — and
// the second is the question that measured 116 GB.
func (s *Stores) Find(digest string) (Ref, bool) {
	for _, x := range s.all {
		if r, ok := x.Find(digest); ok {
			return r, true
		}
	}
	return Ref{}, false
}

func (s *Stores) FindAll(digest string) []Ref {
	var out []Ref
	seen := map[string]bool{}
	for _, x := range s.all {
		r, ok := x.Find(digest)
		if !ok {
			continue
		}
		// Two stores can name the same file — a resolver may already have
		// pointed at the very blob a scan would find. Offering one location
		// twice makes a caller think there are two copies, and makes a failed
		// source get retried against itself.
		if seen[Locator(r)] {
			continue
		}
		seen[Locator(r)] = true
		out = append(out, r)
	}
	return out
}

// Place uses the first store that will take the bytes.
func (s *Stores) Place(digest string, size int64) (Ref, error) {
	var last error
	for _, x := range s.all {
		r, err := x.Place(digest, size)
		if err == nil {
			return r, nil
		}
		last = err
	}
	if last == nil {
		last = fmt.Errorf("%w: no stores are configured", ErrReadOnly)
	}
	return Ref{}, last
}

// Path resolves a ref through whichever store produced it, so a caller holding
// refs from several stores does not have to keep track of which is which.
func (s *Stores) Path(r Ref) string {
	for _, x := range s.all {
		if x.Name() != r.Store {
			continue
		}
		if l, ok := x.(Local); ok {
			return l.Path(r)
		}
	}
	return ""
}

// Commit routes to the store that placed it.
func (s *Stores) Commit(r Ref) error {
	for _, x := range s.all {
		if x.Name() != r.Store {
			continue
		}
		if w, ok := x.(Writable); ok {
			return w.Commit(r)
		}
		return fmt.Errorf("%w: %s cannot take bytes", ErrReadOnly, r.Store)
	}
	return fmt.Errorf("%w: no store named %q", ErrNotFound, r.Store)
}

var (
	_ Store    = (*Stores)(nil)
	_ Local    = (*Stores)(nil)
	_ Writable = (*Stores)(nil)
	_ Store    = (*ContentStore)(nil)
	_ Local    = (*ContentStore)(nil)
	_ Writable = (*ContentStore)(nil)
	_ Store    = (*ForeignStore)(nil)
	_ Local    = (*ForeignStore)(nil)
)
