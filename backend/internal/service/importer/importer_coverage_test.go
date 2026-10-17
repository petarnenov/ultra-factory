package importer

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/apierr"
)

func TestNormalizeImportPathCoverage(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		got, err := normalizeImportPath("")
		if err == nil || got != "" {
			t.Fatalf("got=%q err=%v, want empty required error", got, err)
		}
		if !strings.Contains(err.Error(), "path is required") {
			t.Fatalf("err=%v, want path required", err)
		}
	})
	t.Run("tilde", func(t *testing.T) {
		home, err := os.UserHomeDir()
		if err != nil {
			t.Fatal(err)
		}
		for _, tc := range []struct {
			raw  string
			want string
		}{
			{"~", home},
			{"~/nested", filepath.Join(home, "nested")},
		} {
			got, err := normalizeImportPath(tc.raw)
			if err != nil {
				t.Fatalf("raw=%q: %v", tc.raw, err)
			}
			if got != filepath.Clean(tc.want) {
				t.Fatalf("raw=%q got=%q want=%q", tc.raw, got, filepath.Clean(tc.want))
			}
		}
	})
	t.Run("trims surrounding whitespace", func(t *testing.T) {
		got, err := normalizeImportPath("   /tmp/foo   ")
		if err != nil {
			t.Fatalf("unexpected: %v", err)
		}
		if got != filepath.Clean("/tmp/foo") {
			t.Fatalf("got=%q", got)
		}
	})
}

func TestUnsafeImportPathCoverage(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.MkdirAll(filepath.Join(home, "Desktop"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(home, "Documents"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(home, "Downloads"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(home, ".ao", "data"), 0o755); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name string
		path string
		want bool
	}{
		{"root", "/", true},
		{"home", home, true},
		{"desktop", filepath.Join(home, "Desktop"), true},
		{"documents", filepath.Join(home, "Documents"), true},
		{"downloads", filepath.Join(home, "Downloads"), true},
		{"ao", filepath.Join(home, ".ao"), true},
		{"ao descendant", filepath.Join(home, ".ao", "data"), true},
		{"normal child", filepath.Join(home, "safe-project"), false},
	}
	for _, tc := range cases {
		if got := unsafeImportPath(tc.path); got != tc.want {
			t.Errorf("%s: unsafeImportPath(%q)=%v, want %v", tc.name, tc.path, got, tc.want)
		}
	}
	// home lookup failure returns false for non-root paths.
	t.Setenv("HOME", "")
	if got := unsafeImportPath(filepath.Join(home, "safe-project")); got {
		t.Errorf("expected false when home unavailable, got %v", got)
	}
}

func TestValidateImportRemoteURLCoverage(t *testing.T) {
	cases := []struct {
		name    string
		url     string
		wantErr bool
		code    string
	}{
		{"empty invalid", "", true, "INVALID_GIT_URL"},
		{"whitespace invalid", "   ", true, "INVALID_GIT_URL"},
		{"leading dash invalid", "-https://example.com/repo.git", true, "INVALID_GIT_URL"},
		{"scp empty path", "user@host:", true, "INVALID_GIT_URL"},
		{"scp trailing slash empty", "user@host:/", true, "INVALID_GIT_URL"},
		{"malformed", "://bad", true, "INVALID_GIT_URL"},
		{"https no host", "https:///owner/repo.git", true, "INVALID_GIT_URL"},
		{"https valid", "https://github.com/acme/repository.git", false, ""},
		{"ssh userinfo valid", "ssh://git@github.com/acme/repository.git", false, ""},
		{"git userinfo invalid", "git://token@git.example.test/acme/repository.git", true, "GIT_URL_CONTAINS_CREDENTIALS"},
		{"http password invalid", "https://user:secret@github.com/acme/repository.git", true, "GIT_URL_CONTAINS_CREDENTIALS"},
		{"ssh password invalid", "ssh://git:secret@github.com/acme/repository.git", true, "GIT_URL_CONTAINS_CREDENTIALS"},
		{"file userinfo invalid", "file://token@localhost/tmp/acme/repository.git", true, "GIT_URL_CONTAINS_CREDENTIALS"},
		{"access token query invalid", "https://github.com/acme/repository.git?access_token=secret", true, "GIT_URL_CONTAINS_CREDENTIALS"},
		{"token query invalid", "https://github.com/acme/repository.git?token=secret", true, "GIT_URL_CONTAINS_CREDENTIALS"},
		{"scp valid", "git@github.com:acme/repository.git", false, ""},
		{"file valid", "file:///tmp/acme/repository.git", false, ""},
		{"ssh credential query invalid", "ssh://git@github.com/acme/repository.git?token=secret", true, "GIT_URL_CONTAINS_CREDENTIALS"},
		{"git credential query invalid", "git://git.example.test/acme/repository.git?password=x", true, "GIT_URL_CONTAINS_CREDENTIALS"},
		{"ssh benign query valid", "ssh://git@github.com/acme/repository.git?ref=main", false, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateImportRemoteURL(tc.url)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error")
				}
				var apiErr *apierr.Error
				if !errors.As(err, &apiErr) || (tc.code != "" && apiErr.Code != tc.code) {
					t.Fatalf("err=%v (code=%v), want code %q", err, apiErr, tc.code)
				}
			} else if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestValidateRejectsUnsupportedImportKind(t *testing.T) {
	// Smoke test for the unsupported-import-kind error path in Validate.
	svc := New(Deps{Store: newFakeStore(), Root: filepath.Join(t.TempDir(), "none")})
	_, err := svc.Validate(context.Background(), ImportValidationInput{ImportKind: "bogus", Path: filepath.Join(t.TempDir(), "x")})
	if err == nil {
		t.Fatal("expected error")
	}
	var apiErr *apierr.Error
	if !errors.As(err, &apiErr) || apiErr.Code != "UNSUPPORTED_IMPORT_KIND" {
		t.Fatalf("err=%v, want UNSUPPORTED_IMPORT_KIND", err)
	}
}

func TestPrepareGitUnsupportedImportKindCoverage(t *testing.T) {
	svc := New(Deps{Store: newFakeStore()})
	_, err := svc.PrepareGit(context.Background(), GitPreparationInput{ImportKind: "bogus", Path: t.TempDir()})
	if err == nil {
		t.Fatal("expected error")
	}
	var apiErr *apierr.Error
	if !errors.As(err, &apiErr) || apiErr.Code != "UNSUPPORTED_IMPORT_KIND" {
		t.Fatalf("err=%v, want UNSUPPORTED_IMPORT_KIND", err)
	}
}

func TestDirectChildStatusesAndHelpersCoverage(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	child := filepath.Join(root, "child")
	if err := os.MkdirAll(child, 0o755); err != nil {
		t.Fatal(err)
	}
	plain := filepath.Join(root, "plain")
	if err := os.MkdirAll(plain, 0o755); err != nil {
		t.Fatal(err)
	}
	// unsupported metadata: .git file in a non-repo folder
	if err := os.WriteFile(filepath.Join(child, ".git"), []byte("gitdir: /nope"), 0o644); err != nil {
		t.Fatal(err)
	}
	statuses, err := directChildImportStatuses(ctx, root)
	if err != nil {
		t.Fatalf("directChildImportStatuses: %v", err)
	}
	if len(statuses) != 2 {
		t.Fatalf("statuses = %#v, want 2", statuses)
	}
	var childStatus *RepoGitStatus
	for i := range statuses {
		if statuses[i].RepoPath == child {
			childStatus = &statuses[i]
		}
	}
	if childStatus == nil || !containsString(childStatus.BlockingErrors, "UNSUPPORTED_GIT_METADATA") {
		t.Fatalf("child status = %#v, want UNSUPPORTED_GIT_METADATA", childStatus)
	}

	// directChildImportRepos filters out plain children without blocking errors.
	repos, err := directChildImportRepos(ctx, root)
	if err != nil {
		t.Fatalf("directChildImportRepos: %v", err)
	}
	if len(repos) != 1 || repos[0].RepoPath != child {
		t.Fatalf("repos = %#v, want only child", repos)
	}
}

func TestHasUnsupportedImportGitMetadataCoverage(t *testing.T) {
	root := t.TempDir()
	if hasUnsupportedImportGitMetadata(root) {
		t.Fatal("empty folder should not have unsupported metadata")
	}
	// .git file in non-repo folder
	if err := os.WriteFile(filepath.Join(root, ".git"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !hasUnsupportedImportGitMetadata(root) {
		t.Fatal("expected unsupported git metadata")
	}
}

func TestIsImportFolderEmptyCoverage(t *testing.T) {
	root := t.TempDir()
	if !isImportFolderEmpty(root) {
		t.Fatal("expected empty")
	}
	if err := os.WriteFile(filepath.Join(root, "x"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if isImportFolderEmpty(root) {
		t.Fatal("expected non-empty")
	}
}

func TestActionHelpersCoverage(t *testing.T) {
	if got := actionSet([]string{"a", "a"}); len(got) != 1 {
		t.Fatalf("actionSet=%#v", got)
	}
	if !containsAction([]string{"a"}, "a") {
		t.Fatal("expected contains")
	}
	if containsAction([]string{"a"}, "b") {
		t.Fatal("expected not contains")
	}
}

func containsString(values []string, want string) bool {
	for _, v := range values {
		if v == want {
			return true
		}
	}
	return false
}
