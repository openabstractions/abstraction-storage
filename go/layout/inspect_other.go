//go:build !unix && !windows

package layout

import (
	"fmt"
	"runtime"
)

func readEntry(string, bool) (Entry, error) {
	return Entry{}, fmt.Errorf("no owner check on %s", runtime.GOOS)
}
