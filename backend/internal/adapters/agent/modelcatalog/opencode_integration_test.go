package modelcatalog

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

// resolveRealOpenCode finds an opencode binary the way the daemon would, so the
// integration tests run the user's actual CLI. It skips (never fails) when no
// opencode is installed, keeping CI green on machines without it.
func resolveRealOpenCode(t *testing.T) string {
	t.Helper()
	if path, err := exec.LookPath("opencode"); err == nil && path != "" {
		return path
	}
	if home, err := os.UserHomeDir(); err == nil {
		candidate := filepath.Join(home, ".opencode", "bin", "opencode")
		if info, err := os.Stat(candidate); err == nil && info.Mode()&0o111 != 0 {
			return candidate
		}
	}
	t.Skip("opencode binary not installed; skipping live discovery integration test")
	return ""
}

// TestOpenCodeDiscoveryRunsAgainstRealBinary is the regression guard for the
// "Unrecognized flag: --pure" failure: it runs the exact command the daemon
// builds against the installed opencode and asserts a non-empty catalog.
func TestOpenCodeDiscoveryRunsAgainstRealBinary(t *testing.T) {
	binary := resolveRealOpenCode(t)
	catalog, err := (Discoverer{}).Discover(context.Background(), ports.AgentModelDiscoveryRequest{
		AgentID: "opencode",
		Binary:  binary,
	})
	if err != nil {
		t.Fatalf("live opencode discovery failed: %v", err)
	}
	if len(catalog.Models) == 0 {
		t.Fatalf("live opencode discovery returned no models")
	}
}

// TestOpenCodeCredentialScopeMarksProviderEnv records the provider env the real
// CLI receives, so the assertion does not depend on the host's provider config.
func TestOpenCodeCredentialScopeMarksProviderEnv(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("wrapper fixture is a shell script")
	}
	binary := resolveRealOpenCode(t)
	dir := t.TempDir()
	record := filepath.Join(dir, "provider-env")
	wrapper := filepath.Join(dir, "opencode")
	script := "#!/bin/sh\nprintf '%s' \"$ANTHROPIC_API_KEY\" > '" + record + "'\nexec '" + binary + "' \"$@\"\n"
	if err := os.WriteFile(wrapper, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	catalog, err := (Discoverer{}).Discover(context.Background(), ports.AgentModelDiscoveryRequest{
		AgentID:        "opencode",
		Binary:         wrapper,
		CredentialType: "anthropic_api_key",
	})
	if err != nil {
		t.Fatalf("credential-scoped opencode discovery failed: %v", err)
	}
	if len(catalog.Models) == 0 {
		t.Fatalf("credential-scoped opencode discovery returned no models")
	}
	got, err := os.ReadFile(record)
	if err != nil {
		t.Fatalf("provider env was not passed to opencode: %v", err)
	}
	if string(got) != modelDiscoveryPresenceValue {
		t.Fatalf("ANTHROPIC_API_KEY = %q, want the presence placeholder", got)
	}
}
