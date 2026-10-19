//go:build !unix

package layout

// ownerOnlyBase has no per-user base folder to judge here: on Windows the
// socket and tmp roots lie under the account's own profile folders.
func ownerOnlyBase(string) bool { return false }
