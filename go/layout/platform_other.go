//go:build !unix && !windows

package layout

import "runtime"

// Current is this process's Platform; Resolve refuses it with
// UnsupportedPlatform.
func Current() Platform { return Platform{GOOS: runtime.GOOS} }
