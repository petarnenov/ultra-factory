package fsbrowser

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/apierr"
)

func TestListUsesHomeDirectoryWhenPathIsEmpty(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.Mkdir(filepath.Join(home, "projects"), 0o755); err != nil {
		t.Fatal(err)
	}

	listing, err := New().List(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if listing.Path != home || listing.Parent != filepath.Dir(home) {
		t.Fatalf("listing location = (%q, %q), want (%q, %q)", listing.Path, listing.Parent, home, filepath.Dir(home))
	}
	if len(listing.Entries) != 1 || listing.Entries[0].Name != "projects" {
		t.Fatalf("entries = %#v, want only projects", listing.Entries)
	}
}

func TestListFiltersAndIdentifiesRepositories(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"plain", "directory-repo", "file-repo", ".hidden"} {
		if err := os.Mkdir(filepath.Join(root, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(root, "directory-repo", ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "file-repo", ".git"), []byte("gitdir: ../.git/worktrees/file-repo"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "ordinary-file"), []byte("ignored"), 0o600); err != nil {
		t.Fatal(err)
	}

	uncleanPath := root + string(filepath.Separator) + "nested" + string(filepath.Separator) + ".."
	listing, err := New().List(context.Background(), uncleanPath)
	if err != nil {
		t.Fatal(err)
	}
	if listing.Path != root || listing.Parent != filepath.Dir(root) {
		t.Fatalf("listing location = (%q, %q), want (%q, %q)", listing.Path, listing.Parent, root, filepath.Dir(root))
	}
	if listing.Truncated || len(listing.Entries) != 3 {
		t.Fatalf("listing has %d entries and truncated=%t, want 3 and false", len(listing.Entries), listing.Truncated)
	}
	wantRepos := map[string]bool{"directory-repo": true, "file-repo": true, "plain": false}
	for _, entry := range listing.Entries {
		if entry.GitRepo != wantRepos[entry.Name] {
			t.Errorf("%s GitRepo = %t, want %t", entry.Name, entry.GitRepo, wantRepos[entry.Name])
		}
		if entry.Path != filepath.Join(root, entry.Name) {
			t.Errorf("%s path = %q", entry.Name, entry.Path)
		}
	}
}

func TestListTruncatesAtMaximumEntries(t *testing.T) {
	root := t.TempDir()
	for i := 0; i <= maxEntries; i++ {
		if err := os.Mkdir(filepath.Join(root, fmt.Sprintf("dir-%03d", i)), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	listing, err := New().List(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if len(listing.Entries) != maxEntries || !listing.Truncated {
		t.Fatalf("listing has %d entries and truncated=%t, want %d and true", len(listing.Entries), listing.Truncated, maxEntries)
	}
}

func TestListRejectsCanceledContextBeforeFilesystemAccess(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	listing, err := New().List(ctx, "relative")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
	if listing.Path != "" || listing.Entries != nil {
		t.Fatalf("canceled listing = %#v, want zero value", listing)
	}
}

func TestListMapsInvalidFilesystemPaths(t *testing.T) {
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name string
		path string
		code string
	}{
		{name: "relative", path: "relative/path", code: "FS_PATH_NOT_ABSOLUTE"},
		{name: "missing", path: filepath.Join(t.TempDir(), "missing"), code: "FS_NOT_FOUND"},
		{name: "regular file", path: file, code: "FS_NOT_A_DIRECTORY"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := New().List(context.Background(), tc.path)
			var apiError *apierr.Error
			if !errors.As(err, &apiError) || apiError.Code != tc.code {
				t.Fatalf("error = %v, want API error %s", err, tc.code)
			}
		})
	}
}
