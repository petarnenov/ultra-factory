//go:build !e2e

package cli

import (
	"os"
	"strings"
	"testing"
)

// TestMain strips the inherited AO_* variables so the package passes inside an
// AO worker session. Tests that need a value set it explicitly with t.Setenv.
func TestMain(m *testing.M) {
	for _, kv := range os.Environ() {
		if name, _, _ := strings.Cut(kv, "="); strings.HasPrefix(name, "AO_") {
			os.Unsetenv(name)
		}
	}
	os.Exit(m.Run())
}
