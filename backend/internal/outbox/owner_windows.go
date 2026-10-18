//go:build windows

package outbox

import "os"

// checkOwner relies on the per-user profile directories that hold the data
// and temporary directories on Windows.
func checkOwner(string, os.FileInfo) error { return nil }
