//go:build !darwin

package layout

// excludeFromTimeMachine has nothing to do outside macOS.
func excludeFromTimeMachine(string) error { return nil }
