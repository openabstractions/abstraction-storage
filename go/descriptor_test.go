package storage

import (
	api "github.com/openabstractions/abstraction-storage/go/abstraction/storage/api"
	"testing"
)

// The binding boundary retains provider location as opaque data; consumers use
// the separately selected Local extension when they need a filesystem path.
func TestDescriptorPreservesProviderReference(t *testing.T) {
	native := NewRef("fixture", "sha256:test", "opaque provider location", 42)
	record := api.Ref{Store: native.Store, Digest: native.Digest, Size: native.Size, Locator: Locator(native)}
	decoded, err := api.Decode(api.Encode(&record))
	if err != nil {
		t.Fatal(err)
	}
	restored := NewRef(decoded.Store, decoded.Digest, decoded.Locator, decoded.Size)
	if restored != native {
		t.Fatalf("reference changed: %+v", restored)
	}
}
