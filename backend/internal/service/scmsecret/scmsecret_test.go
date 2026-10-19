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

func TestGitHubTokenRoundTripPerHost(t *testing.T) {
	store := New(t.TempDir())
	ctx := context.Background()
	if err := store.SetGitHubToken(ctx, "github.com", "work-account", fakeToken); err != nil {
		t.Fatalf("SetGitHubToken: %v", err)
	}
	if err := store.SetGitHubToken(ctx, "ghe.example.com", "work-account", "enterprise-token"); err != nil {
		t.Fatalf("SetGitHubToken enterprise: %v", err)
	}
	public, err := store.GitHubToken(ctx, "github.com", "work-account")
	if err != nil || public != fakeToken {
		t.Fatalf("public token = (%q, %v)", public, err)
	}
	enterprise, err := store.GitHubToken(ctx, "ghe.example.com", "work-account")
	if err != nil || enterprise != "enterprise-token" {
		t.Fatalf("enterprise token = (%q, %v)", enterprise, err)
	}
	path := filepath.Join(store.dataDir, "secrets", "github", "github.com", "work-account")
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

func TestGitHubTokenDefaultsToGitHubCom(t *testing.T) {
	store := New(t.TempDir())
	ctx := context.Background()
	if err := store.SetGitHubToken(ctx, "", "work-account", fakeToken); err != nil {
		t.Fatalf("SetGitHubToken: %v", err)
	}
	got, err := store.GitHubToken(ctx, "github.com", "work-account")
	if err != nil || got != fakeToken {
		t.Fatalf("token = (%q, %v)", got, err)
	}
}

func TestGitHubTokenMissing(t *testing.T) {
	store := New(t.TempDir())
	_, err := store.GitHubToken(context.Background(), "github.com", "nobody")
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
	if err := store.SetGitHubToken(ctx, "github.com", "work-account", fakeToken); err != nil {
		t.Fatalf("SetGitHubToken: %v", err)
	}
	path := filepath.Join(store.dataDir, "secrets", "github", "github.com", "work-account")
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	_, err := store.GitHubToken(ctx, "github.com", "work-account")
	if !errors.Is(err, ErrInsecurePermissions) {
		t.Fatalf("err = %v, want ErrInsecurePermissions", err)
	}
	if strings.Contains(err.Error(), fakeToken) {
		t.Fatal("permission error leaked the token")
	}
}

func TestListGitHubAccountsNeverReturnsValues(t *testing.T) {
	store := New(t.TempDir())
	ctx := context.Background()
	for _, account := range []struct{ host, login string }{
		{"github.com", "hobby"},
		{"github.com", "Work-Account"},
		{"ghe.example.com", "work-account"},
	} {
		if err := store.SetGitHubToken(ctx, account.host, account.login, fakeToken); err != nil {
			t.Fatalf("SetGitHubToken(%+v): %v", account, err)
		}
	}
	got, err := store.ListGitHubAccounts(ctx)
	if err != nil {
		t.Fatalf("ListGitHubAccounts: %v", err)
	}
	want := []Account{
		{Host: "ghe.example.com", Login: "work-account"},
		{Host: "github.com", Login: "hobby"},
		{Host: "github.com", Login: "work-account"},
	}
	if len(got) != len(want) {
		t.Fatalf("accounts = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("accounts = %+v, want %+v", got, want)
		}
	}
	if strings.Contains(strings.Join([]string{got[0].Login, got[1].Login, got[2].Login}, ""), fakeToken) {
		t.Fatal("list returned a token value")
	}
}

func TestCaseInsensitiveHostAndLogin(t *testing.T) {
	store := New(t.TempDir())
	ctx := context.Background()
	if err := store.SetGitHubToken(ctx, "GHE.Example.com", "Work-Account", fakeToken); err != nil {
		t.Fatalf("SetGitHubToken: %v", err)
	}
	got, err := store.GitHubToken(ctx, "ghe.example.com", "work-account")
	if err != nil || got != fakeToken {
		t.Fatalf("case-insensitive lookup = (%q, %v)", got, err)
	}
}

func TestDeleteGitHubTokenIsIdempotent(t *testing.T) {
	store := New(t.TempDir())
	ctx := context.Background()
	if err := store.SetGitHubToken(ctx, "github.com", "work-account", fakeToken); err != nil {
		t.Fatalf("SetGitHubToken: %v", err)
	}
	if err := store.DeleteGitHubToken(ctx, "github.com", "work-account"); err != nil {
		t.Fatalf("DeleteGitHubToken: %v", err)
	}
	if err := store.DeleteGitHubToken(ctx, "github.com", "work-account"); err != nil {
		t.Fatalf("second DeleteGitHubToken: %v", err)
	}
	if _, err := store.GitHubToken(ctx, "github.com", "work-account"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestRejectsUnsafeHostsAndLogins(t *testing.T) {
	store := New(t.TempDir())
	for _, tc := range []struct{ host, login string }{
		{"", ""},
		{"https://github.com", "work-account"},
		{"github.com/path", "work-account"},
		{"github.com", "../escape"},
		{"github.com", "not_a_token"},
		{"github.com", "bad login"},
	} {
		if err := store.SetGitHubToken(context.Background(), tc.host, tc.login, fakeToken); err == nil {
			t.Fatalf("SetGitHubToken(%+v) = nil, want error", tc)
		}
	}
}

func TestRejectsMultilineToken(t *testing.T) {
	store := New(t.TempDir())
	err := store.SetGitHubToken(context.Background(), "github.com", "work-account", "first\nsecond")
	if err == nil {
		t.Fatal("multiline token accepted")
	}
	if strings.Contains(err.Error(), "first") {
		t.Fatal("token value leaked in error")
	}
}
