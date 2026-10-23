//go:build !windows

package e2e

import (
	"os"
	"strings"
	"testing"
)

func TestDaemonChildEnvironmentUsesOnlyScratchAOState(t *testing.T) {
	t.Setenv("AO_SESSION_ID", "invoking-worker")
	workerSessionID := os.Getenv("AO_SESSION_ID")
	inherited := []string{
		"PATH=/usr/bin",
		"HOME=/home/petar",
		"AO_SESSION_ID=worker-session",
		"AO_DATA_DIR=/home/petar/.ao/data",
		"AO_RUN_FILE=/home/petar/.ao/running.json",
		"AO_PORT=3001",
		"AO_PROFILE=/home/petar/.ao/profile",
		"AO_E2E_TEST_BINARY=/home/petar/.ao/production-test-binary",
	}
	testAOEnv := map[string]string{
		"AO_E2E_TEST_BINARY":       "/tmp/ao-e2e-test-binary",
		"AO_E2E_FAKE_APPROVAL_ACP": "1",
		"AO_E2E_ACP_CALLS":         "/tmp/ao-e2e-calls.log",
	}
	got := daemonChildEnvironment(inherited, "/tmp/ao-e2e-data", 43123, testAOEnv)
	if os.Getenv("AO_SESSION_ID") != workerSessionID {
		t.Fatal("constructing a disposable child environment changed the worker session")
	}
	values := make(map[string]string, len(got))
	for _, entry := range got {
		name, value, ok := strings.Cut(entry, "=")
		if !ok {
			t.Fatalf("environment entry %q has no value", entry)
		}
		if _, exists := values[name]; exists {
			t.Fatalf("duplicate environment entry %q", name)
		}
		values[name] = value
	}
	if values["PATH"] != "/usr/bin" || values["HOME"] != "/home/petar" {
		t.Fatalf("non-AO environment was not preserved: %#v", values)
	}
	if _, ok := values["AO_SESSION_ID"]; ok {
		t.Fatal("worker session identity leaked into the disposable daemon")
	}
	if values["AO_DATA_DIR"] != "/tmp/ao-e2e-data" ||
		values["AO_RUN_FILE"] != "/tmp/ao-e2e-data/running.json" ||
		values["AO_PORT"] != "43123" {
		t.Fatalf("disposable daemon AO settings = %#v", values)
	}
	for name, value := range testAOEnv {
		if values[name] != value {
			t.Fatalf("test-owned fixture %s = %q, want %q", name, values[name], value)
		}
	}
	for name, value := range values {
		if strings.HasPrefix(name, "AO_") && name != "AO_DATA_DIR" && name != "AO_RUN_FILE" && name != "AO_PORT" {
			if _, ok := testAOEnv[name]; ok {
				continue
			}
			t.Fatalf("unexpected inherited AO setting %s=%s", name, value)
		}
	}
}
