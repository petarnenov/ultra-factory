package scmsecret

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const fakeToken = "fake-test-token-value"

func TestGitHubTokenRoundTrip(t *testing.T) {
	store := New(t.TempDir())
	ctx := context.Background()
	if err := store.SetGitHubToken(ctx, "work-account", fakeToken); err != nil {
		t.Fatalf("SetGitHubToken: %v", err)
	}
	got, err := store.GitHubToken(ctx, "work-account")
	if err != nil {
		t.Fatalf("GitHubToken: %v", err)
	}
	if got != fakeToken {
		t.Fatalf("token = %q, want stored token", got)
	}
	path := filepath.Join(store.dataDir, "secrets", "github", "work-account")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat token: %v", err)
	}
	if runtime.GOOS != "windows" {
		if mode := info.Mode().Perm(); mode != 0o600 {
			t.Fatalf("token mode = %04o, want 0600", mode)
		}
		dirInfo, err := os.Stat(filepath.Dir(path))
		if err != nil {
			t.Fatalf("stat secret dir: %v", err)
		}
		if mode := dirInfo.Mode().Perm(); mode != 0o700 {
			t.Fatalf("secret dir mode = %04o, want 0700", mode)
		}
	}
}

func TestGitHubTokenMissing(t *testing.T) {
	store := New(t.TempDir())
	_, err := store.GitHubToken(context.Background(), "nobody")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestGitHubTokenRejectsInsecureFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows does not expose POSIX mode bits through os.FileMode")
	}
	store := New(t.TempDir())
	ctx := context.Background()
	if err := store.SetGitHubToken(ctx, "work-account", fakeToken); err != nil {
		t.Fatalf("SetGitHubToken: %v", err)
	}
	path := filepath.Join(store.dataDir, "secrets", "github", "work-account")
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	_, err := store.GitHubToken(ctx, "work-account")
	if !errors.Is(err, ErrInsecurePermissions) {
		t.Fatalf("err = %v, want ErrInsecurePermissions", err)
	}
	if strings.Contains(err.Error(), fakeToken) {
		t.Fatal("permission error leaked the token")
	}
}

func TestListGitHubLoginsNeverReturnsValues(t *testing.T) {
	store := New(t.TempDir())
	ctx := context.Background()
	for _, login := range []string{"hobby", "Work-Account"} {
		if err := store.SetGitHubToken(ctx, login, fakeToken); err != nil {
			t.Fatalf("SetGitHubToken(%s): %v", login, err)
		}
	}
	got, err := store.GitHubToken(ctx, "work-account")
	if err != nil || got != fakeToken {
		t.Fatalf("case-insensitive lookup = (%q, %v)", got, err)
	}
	logins, err := store.ListGitHubLogins(ctx)
	if err != nil {
		t.Fatalf("ListGitHubLogins: %v", err)
	}
	if len(logins) != 2 || logins[0] != "hobby" || logins[1] != "work-account" {
		t.Fatalf("logins = %v, want [hobby work-account]", logins)
	}
	for _, login := range logins {
		if strings.Contains(login, fakeToken) {
			t.Fatal("list returned a token value")
		}
	}
}

func TestDeleteGitHubTokenIsIdempotent(t *testing.T) {
	store := New(t.TempDir())
	ctx := context.Background()
	if err := store.SetGitHubToken(ctx, "work-account", fakeToken); err != nil {
		t.Fatalf("SetGitHubToken: %v", err)
	}
	if err := store.DeleteGitHubToken(ctx, "work-account"); err != nil {
		t.Fatalf("DeleteGitHubToken: %v", err)
	}
	if err := store.DeleteGitHubToken(ctx, "work-account"); err != nil {
		t.Fatalf("second DeleteGitHubToken: %v", err)
	}
	if _, err := store.GitHubToken(ctx, "work-account"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestRejectsUnsafeLogins(t *testing.T) {
	store := New(t.TempDir())
	for _, login := range []string{"", " ", "../escape", "ghp_not_a_valid_login", "bad login"} {
		if err := store.SetGitHubToken(context.Background(), login, fakeToken); err == nil {
			t.Fatalf("SetGitHubToken(%q) = nil, want error", login)
		}
	}
}

func TestRejectsMultilineToken(t *testing.T) {
	store := New(t.TempDir())
	err := store.SetGitHubToken(context.Background(), "work-account", "first\nsecond")
	if err == nil {
		t.Fatal("multiline token accepted")
	}
	if strings.Contains(err.Error(), "first") {
		t.Fatal("token value leaked in error")
	}
}
