package devimport

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/apierr"
)

func TestRunProjectsEmptySourceDataDir(t *testing.T) {
	ctx := context.Background()
	targetDir := filepath.Join(t.TempDir(), "target")
	target := openStore(t, targetDir)
	svc := New(Deps{Store: target, TargetDataDir: targetDir, OpenSource: openReadOnlySource})

	_, err := svc.RunProjects(ctx, RunInput{SourceDataDir: "", DryRun: true})
	if err == nil {
		t.Fatal("expected error for empty source")
	}
	var apiErr *apierr.Error
	if !errors.As(err, &apiErr) || apiErr.Code != "SOURCE_DATA_DIR_REQUIRED" {
		t.Fatalf("err = %v, want SOURCE_DATA_DIR_REQUIRED", err)
	}
}

func TestRunProjectsNilOpenSource(t *testing.T) {
	ctx := context.Background()
	sourceDir := filepath.Join(t.TempDir(), "source")
	targetDir := filepath.Join(t.TempDir(), "target")
	if err := os.MkdirAll(sourceDir, 0o755); err != nil {
		t.Fatal(err)
	}
	target := openStore(t, targetDir)
	svc := New(Deps{Store: target, TargetDataDir: targetDir, OpenSource: nil})

	_, err := svc.RunProjects(ctx, RunInput{SourceDataDir: sourceDir, DryRun: true})
	if err == nil || !strings.Contains(err.Error(), "open source store: dependency is nil") {
		t.Fatalf("err = %v, want dependency nil", err)
	}
}

func TestRunProjectsOpenSourceError(t *testing.T) {
	ctx := context.Background()
	sourceDir := filepath.Join(t.TempDir(), "source")
	targetDir := filepath.Join(t.TempDir(), "target")
	if err := os.MkdirAll(sourceDir, 0o755); err != nil {
		t.Fatal(err)
	}
	target := openStore(t, targetDir)
	svc := New(Deps{
		Store:         target,
		TargetDataDir: targetDir,
		OpenSource: func(context.Context, string) (SourceStore, error) {
			return nil, errors.New("open failed")
		},
	})

	_, err := svc.RunProjects(ctx, RunInput{SourceDataDir: sourceDir, DryRun: true})
	if err == nil || !strings.Contains(err.Error(), "open source store: open failed") {
		t.Fatalf("err = %v, want wrapped open error", err)
	}
}

func TestRunProjectsEmptyTargetDataDir(t *testing.T) {
	ctx := context.Background()
	sourceDir := filepath.Join(t.TempDir(), "source")
	targetDir := filepath.Join(t.TempDir(), "target")
	if err := os.MkdirAll(sourceDir, 0o755); err != nil {
		t.Fatal(err)
	}
	target := openStore(t, targetDir)
	svc := New(Deps{Store: target, TargetDataDir: "", OpenSource: func(context.Context, string) (SourceStore, error) {
		t.Fatal("openSource should not be called when target dir is invalid")
		return nil, nil
	}})

	_, err := svc.RunProjects(ctx, RunInput{SourceDataDir: sourceDir, DryRun: true})
	if err == nil {
		t.Fatal("expected target resolve error")
	}
	var apiErr *apierr.Error
	if !errors.As(err, &apiErr) || apiErr.Code != "SOURCE_DATA_DIR_REQUIRED" {
		t.Fatalf("err = %v, want SOURCE_DATA_DIR_REQUIRED", err)
	}
}

func TestResolveDataDirCases(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		got, err := resolveDataDir("")
		if err == nil || got != "" {
			t.Fatalf("got=%q err=%v, want empty error", got, err)
		}
		var apiErr *apierr.Error
		if !errors.As(err, &apiErr) || apiErr.Code != "SOURCE_DATA_DIR_REQUIRED" {
			t.Fatalf("err = %v, want SOURCE_DATA_DIR_REQUIRED", err)
		}
	})
	t.Run("whitespace", func(t *testing.T) {
		got, err := resolveDataDir("   ")
		if err == nil || got != "" {
			t.Fatalf("got=%q err=%v, want empty error", got, err)
		}
	})
	t.Run("missing path returns cleaned absolute", func(t *testing.T) {
		missing := filepath.Join(t.TempDir(), "does-not-exist")
		got, err := resolveDataDir(missing)
		if err != nil {
			t.Fatalf("resolveDataDir: %v", err)
		}
		if got != filepath.Clean(missing) {
			t.Fatalf("got=%q want=%q", got, filepath.Clean(missing))
		}
	})
	t.Run("existing path resolves symlinks", func(t *testing.T) {
		dir := t.TempDir()
		got, err := resolveDataDir(dir)
		if err != nil {
			t.Fatalf("resolveDataDir: %v", err)
		}
		resolved, _ := filepath.EvalSymlinks(dir)
		if got != filepath.Clean(resolved) {
			t.Fatalf("got=%q want=%q", got, filepath.Clean(resolved))
		}
	})
}

func TestSameDataDirCases(t *testing.T) {
	t.Run("both missing equal", func(t *testing.T) {
		p := filepath.Join(t.TempDir(), "missing")
		same, err := sameDataDir(p, p)
		if err != nil || !same {
			t.Fatalf("same=%v err=%v, want true", same, err)
		}
	})
	t.Run("both missing different", func(t *testing.T) {
		a := filepath.Join(t.TempDir(), "a")
		b := filepath.Join(t.TempDir(), "b")
		same, err := sameDataDir(a, b)
		if err != nil || same {
			t.Fatalf("same=%v err=%v, want false", same, err)
		}
	})
	t.Run("same directory file", func(t *testing.T) {
		dir := t.TempDir()
		same, err := sameDataDir(dir, dir)
		if err != nil || !same {
			t.Fatalf("same=%v err=%v, want true", same, err)
		}
	})
	t.Run("different existing directories", func(t *testing.T) {
		a := t.TempDir()
		b := t.TempDir()
		same, err := sameDataDir(a, b)
		if err != nil || same {
			t.Fatalf("same=%v err=%v, want false", same, err)
		}
	})
}

func TestSameDataDirErrors(t *testing.T) {
	t.Run("source path component is not directory", func(t *testing.T) {
		file := filepath.Join(t.TempDir(), "notdir")
		if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		_, err := sameDataDir(filepath.Join(file, "child"), filepath.Join(t.TempDir(), "other"))
		if err == nil {
			t.Fatal("expected error")
		}
		var apiErr *apierr.Error
		if !errors.As(err, &apiErr) || apiErr.Code != "INVALID_DATA_DIR" {
			t.Fatalf("err=%v want INVALID_DATA_DIR", err)
		}
	})
	t.Run("target path component is not directory", func(t *testing.T) {
		file := filepath.Join(t.TempDir(), "notdir")
		if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		src := t.TempDir()
		_, err := sameDataDir(src, filepath.Join(file, "child"))
		if err == nil {
			t.Fatal("expected error")
		}
		var apiErr *apierr.Error
		if !errors.As(err, &apiErr) || apiErr.Code != "INVALID_DATA_DIR" {
			t.Fatalf("err=%v want INVALID_DATA_DIR", err)
		}
	})
}
