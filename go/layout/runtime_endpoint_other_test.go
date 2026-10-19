//go:build !unix

package layout

import "testing"

func TestRuntimeEndpointRootUnsupported(t *testing.T) {
	root, err := RuntimeEndpointRoot()
	refusal, ok := err.(*Refusal)
	if root != "" || !ok || refusal.Reason != UnsupportedPlatform {
		t.Fatalf("unsupported platform fabricated control root: %q %v", root, err)
	}
}
