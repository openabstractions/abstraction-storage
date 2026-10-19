//go:build !unix

package layout

// RuntimeEndpointRoot resolves a Unix control-socket namespace. Other platforms
// retain their native endpoint naming and have no filesystem root for this API.
func RuntimeEndpointRoot() (string, error) {
	return "", refuse(UnsupportedPlatform, "", "runtime control sockets require Unix")
}
