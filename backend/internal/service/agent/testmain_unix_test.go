//go:build unix

package agent

import (
	"os"
	"syscall"
	"testing"
)

// TestMain pins umask 022 so t.TempDir() subdirectories are not group-writable
// like CI. A developer umask of 002 otherwise trips the Codex vault check.
func TestMain(m *testing.M) {
	syscall.Umask(0o022)
	os.Exit(m.Run())
}
