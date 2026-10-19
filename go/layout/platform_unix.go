//go:build unix

package layout

import (
	"os"
	"runtime"
)

// Current is this process's Platform: $HOME, the XDG variables and $TMPDIR
// from the environment, and the sandbox it runs in, if any.
func Current() Platform {
	return Platform{GOOS: runtime.GOOS, Getenv: os.Getenv, ReadFile: os.ReadFile, UID: os.Getuid(), Sandbox: sandbox()}
}

// sandbox names an App Sandbox container on macOS, a Flatpak or a Snap on
// Linux. Each is read from what the sandbox itself sets; a forged value only
// makes this process refuse.
func sandbox() string {
	switch {
	case runtime.GOOS == "darwin" && os.Getenv("APP_SANDBOX_CONTAINER_ID") != "":
		return "app-sandbox"
	case runtime.GOOS == "linux" && exists("/.flatpak-info"):
		return "flatpak"
	case runtime.GOOS == "linux" && os.Getenv("SNAP") != "":
		return "snap"
	}
	return ""
}

func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
