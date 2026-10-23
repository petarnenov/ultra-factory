package modelcatalog

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
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

func resolveIsolatedOpenCode(t *testing.T) (string, map[string]string) {
	t.Helper()
	binary := resolveRealOpenCode(t)
	if runtime.GOOS == "windows" {
		return binary, nil
	}

	root := t.TempDir()
	env := map[string]string{
		"HOME":                        root,
		"XDG_CONFIG_HOME":             filepath.Join(root, "config"),
		"XDG_DATA_HOME":               filepath.Join(root, "data"),
		"XDG_CACHE_HOME":              filepath.Join(root, "cache"),
		"XDG_STATE_HOME":              filepath.Join(root, "state"),
		"OPENCODE_CONFIG":             filepath.Join(root, "opencode.json"),
		"OPENCODE_CONFIG_DIR":         filepath.Join(root, "config", "opencode"),
		"OPENCODE_DB":                 filepath.Join(root, "data", "opencode.db"),
		"OPENCODE_DISABLE_AUTOUPDATE": "true",
	}
	for _, key := range []string{"XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_CACHE_HOME", "XDG_STATE_HOME", "OPENCODE_CONFIG_DIR"} {
		if err := os.MkdirAll(env[key], 0o700); err != nil {
			t.Fatalf("create isolated OpenCode directory: %v", err)
		}
	}
	if err := os.WriteFile(env["OPENCODE_CONFIG"], []byte("{\"$schema\":\"https://opencode.ai/config.json\"}\n"), 0o600); err != nil {
		t.Fatalf("write isolated OpenCode config: %v", err)
	}

	probe := exec.Command(binary, "models", "--help")
	probe.Env = mergedEnvironment(os.Environ(), env)
	help, err := probe.CombinedOutput()
	if err != nil || !strings.Contains(string(help), "--standalone") {
		return binary, env
	}

	wrapper := filepath.Join(root, "opencode")
	script := fmt.Sprintf("#!/bin/sh\nexec %s \"$@\" --standalone\n", shellQuote(binary))
	if err := os.WriteFile(wrapper, []byte(script), 0o700); err != nil {
		t.Fatalf("write private OpenCode wrapper: %v", err)
	}
	return wrapper, env
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

// TestOpenCodeDiscoveryRunsAgainstRealBinary checks the daemon's model args
// against the installed CLI and asserts a non-empty catalog.
func TestOpenCodeDiscoveryRunsAgainstRealBinary(t *testing.T) {
	binary, env := resolveIsolatedOpenCode(t)
	catalog, err := (Discoverer{}).Discover(context.Background(), ports.AgentModelDiscoveryRequest{
		AgentID: "opencode",
		Binary:  binary,
		Env:     env,
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
	binary, env := resolveIsolatedOpenCode(t)
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
		Env:            env,
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
