//go:build !windows

package persistenthost

// sharingViolation is never true off Windows, where files are not locked
// against readers.
func sharingViolation(error) bool { return false }
