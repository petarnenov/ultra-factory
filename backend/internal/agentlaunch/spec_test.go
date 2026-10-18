package agentlaunch

import (
	"os"
	"strings"
	"testing"
)

func TestWriteTempAndReadAndRemoveRoundTrip(t *testing.T) {
	want := Spec{
		WorkspacePath: "/work/<agent>&repo",
		Argv:          []string{"agent", "--prompt", "<hello>"},
		FallbackArgv:  []string{"fallback", "--mode", "safe"},
	}

	path, err := WriteTemp(want)
	if err != nil {
		t.Fatalf("WriteTemp() error = %v", err)
	}
	t.Cleanup(func() { _ = os.Remove(path) })

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read written spec: %v", err)
	}
	if strings.Contains(string(raw), `\u003c`) || strings.Contains(string(raw), `\u0026`) {
		t.Fatalf("launch spec escaped HTML characters: %s", raw)
	}

	got, err := ReadAndRemove(path)
	if err != nil {
		t.Fatalf("ReadAndRemove() error = %v", err)
	}
	if got.WorkspacePath != want.WorkspacePath || !equalStrings(got.Argv, want.Argv) || !equalStrings(got.FallbackArgv, want.FallbackArgv) {
		t.Fatalf("ReadAndRemove() = %#v, want %#v", got, want)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("spec file stat error = %v, want file to be removed", err)
	}
}

func TestReadAndRemoveRejectsUnreadableAndMalformedSpecs(t *testing.T) {
	if _, err := ReadAndRemove(t.TempDir() + "/missing.json"); err == nil || !strings.Contains(err.Error(), "read launch spec") {
		t.Fatalf("ReadAndRemove(missing) error = %v, want read error", err)
	}

	path := t.TempDir() + "/malformed.json"
	if err := os.WriteFile(path, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadAndRemove(path); err == nil || !strings.Contains(err.Error(), "parse launch spec") {
		t.Fatalf("ReadAndRemove(malformed) error = %v, want parse error", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("malformed spec stat error = %v, want file to be removed", err)
	}
}

func TestReadAndRemoveRequiresArgv(t *testing.T) {
	path := t.TempDir() + "/empty-argv.json"
	if err := os.WriteFile(path, []byte(`{"workspacePath":"/work","fallbackArgv":["fallback"]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadAndRemove(path); err == nil || !strings.Contains(err.Error(), "argv is required") {
		t.Fatalf("ReadAndRemove(empty argv) error = %v, want argv validation error", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("empty-argv spec stat error = %v, want file to be removed", err)
	}
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}
