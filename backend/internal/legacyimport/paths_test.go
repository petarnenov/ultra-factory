package legacyimport

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestDefaultLegacyRootDir_Success(t *testing.T) {
	original := userHomeDir
	t.Cleanup(func() { userHomeDir = original })

	userHomeDir = func() (string, error) {
		return "/home/testuser", nil
	}

	got := DefaultLegacyRootDir()
	want := filepath.Join("/home/testuser", ".agent-orchestrator")
	if got != want {
		t.Fatalf("DefaultLegacyRootDir() = %q, want %q", got, want)
	}
}

func TestDefaultLegacyRootDir_HomeLookupFailure(t *testing.T) {
	original := userHomeDir
	t.Cleanup(func() { userHomeDir = original })

	userHomeDir = func() (string, error) {
		return "", errors.New("home lookup failed")
	}

	if got := DefaultLegacyRootDir(); got != "" {
		t.Fatalf("DefaultLegacyRootDir() = %q, want empty string on home lookup failure", got)
	}
}

func TestGlobalConfigPath(t *testing.T) {
	root := filepath.Join("legacy", "root")
	got := globalConfigPath(root)
	want := filepath.Join(root, "config.yaml")
	if got != want {
		t.Fatalf("globalConfigPath(%q) = %q, want %q", root, got, want)
	}
}

func TestPreferencesPath(t *testing.T) {
	root := filepath.Join("legacy", "root")
	got := preferencesPath(root)
	want := filepath.Join(root, "portfolio", "preferences.json")
	if got != want {
		t.Fatalf("preferencesPath(%q) = %q, want %q", root, got, want)
	}
}

func TestRegisteredPath(t *testing.T) {
	root := filepath.Join("legacy", "root")
	got := registeredPath(root)
	want := filepath.Join(root, "portfolio", "registered.json")
	if got != want {
		t.Fatalf("registeredPath(%q) = %q, want %q", root, got, want)
	}
}
