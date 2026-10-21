package remotehost

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// writerHelperEnv names the file a re-executed test binary writes to in
// TestRegistryWriterHelper, so two real processes can contend on one registry.
const writerHelperEnv = "AO_TEST_REGISTRY_WRITER_FILE"

func quietLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func writeRegistry(t *testing.T, path string, entries ...Entry) {
	t.Helper()
	if err := saveEntries(path, entries); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestFilePathHonorsOverrideAndSharesTheMachineFile(t *testing.T) {
	override := filepath.Join(t.TempDir(), "custom.json")
	t.Setenv("AO_REMOTES_FILE", override)
	got, err := FilePath()
	if err != nil || got != override {
		t.Fatalf("override: got %q, %v; want %q", got, err, override)
	}

	t.Setenv("AO_REMOTES_FILE", "")
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("AO_DATA_DIR", filepath.Join(home, ".ao", "dev", "data"))
	got, err = FilePath()
	if err != nil || got != filepath.Join(home, ".ao", "remotes.json") {
		t.Fatalf("dev daemon: got %q, %v", got, err)
	}

	isolated := filepath.Join(t.TempDir(), "lab-data")
	t.Setenv("AO_DATA_DIR", isolated)
	got, err = FilePath()
	if err != nil || got != filepath.Join(isolated, "remotes.json") {
		t.Fatalf("isolated install: got %q, %v", got, err)
	}
}

func TestMigrateMovesLocalFileWhenSharedIsMissing(t *testing.T) {
	dir := t.TempDir()
	local := filepath.Join(dir, "data", "remotes.json")
	shared := filepath.Join(dir, "remotes.json")
	writeRegistry(t, local, Entry{Label: "box", URL: "http://a", Password: "pw", HostID: "h1"})

	report, err := MigrateInstanceFile(local, shared, quietLogger())
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if !report.Moved {
		t.Fatalf("expected moved report, got %+v", report)
	}
	if _, err := os.Stat(local); !os.IsNotExist(err) {
		t.Fatalf("local file should be gone, stat err %v", err)
	}
	entries, err := ReadEntries(shared)
	if err != nil || len(entries) != 1 || entries[0].Password != "pw" {
		t.Fatalf("shared after move: %+v, %v", entries, err)
	}
}

func TestMigrateMergesByHostIDAndSharedWins(t *testing.T) {
	dir := t.TempDir()
	local := filepath.Join(dir, "data", "remotes.json")
	shared := filepath.Join(dir, "remotes.json")
	writeRegistry(t, local,
		Entry{Label: "old-name", URL: "http://a", Password: "stale", HostID: "h1"},
		Entry{Label: "only-local", URL: "http://b", Password: "pw2", HostID: "h2"},
		Entry{Label: "unpaired-local", URL: "http://c", Password: "pw3"},
	)
	writeRegistry(t, shared, Entry{Label: "shared-name", URL: "http://a", Password: "fresh", HostID: "h1"})

	report, err := MigrateInstanceFile(local, shared, quietLogger())
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if report.Moved {
		t.Fatalf("merge must not report a move")
	}
	if strings.Join(report.Merged, ",") != "only-local,unpaired-local" {
		t.Fatalf("merged labels: %v", report.Merged)
	}
	entries, err := ReadEntries(shared)
	if err != nil || len(entries) != 3 {
		t.Fatalf("shared after merge: %+v, %v", entries, err)
	}
	if entries[0].Label != "shared-name" || entries[0].Password != "fresh" {
		t.Fatalf("shared entry must win: %+v", entries[0])
	}
	if _, err := os.Stat(local); !os.IsNotExist(err) {
		t.Fatalf("local file should be removed after merge, stat err %v", err)
	}
}

func TestMigrateIsNoOpWithoutLocalFileOrWhenPathsMatch(t *testing.T) {
	dir := t.TempDir()
	shared := filepath.Join(dir, "remotes.json")
	writeRegistry(t, shared, Entry{Label: "kept", URL: "http://a", Password: "pw", HostID: "h1"})

	if _, err := MigrateInstanceFile(filepath.Join(dir, "missing.json"), shared, quietLogger()); err != nil {
		t.Fatalf("missing local: %v", err)
	}
	if _, err := MigrateInstanceFile(shared, shared, quietLogger()); err != nil {
		t.Fatalf("same path: %v", err)
	}
	entries, err := ReadEntries(shared)
	if err != nil || len(entries) != 1 {
		t.Fatalf("shared changed: %+v, %v", entries, err)
	}
}

func TestUpdateWaitsForALiveLockAndTimesOut(t *testing.T) {
	previous := lockWait
	lockWait = 50 * time.Millisecond
	t.Cleanup(func() { lockWait = previous })

	path := filepath.Join(t.TempDir(), "remotes.json")
	if err := os.WriteFile(LockPath(path), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	err := Update(path, func(entries []Entry) ([]Entry, error) { return entries, nil })
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("expected lock timeout, got %v", err)
	}
	if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
		t.Fatalf("a refused update must not create the file")
	}
}

func TestUpdateBreaksAnAbandonedLock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "remotes.json")
	lock := LockPath(path)
	if err := os.WriteFile(lock, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-2 * lockStale)
	if err := os.Chtimes(lock, old, old); err != nil {
		t.Fatal(err)
	}
	err := Update(path, func(entries []Entry) ([]Entry, error) {
		return append(entries, Entry{Label: "added", URL: "http://a", Password: "pw", HostID: "h1"}), nil
	})
	if err != nil {
		t.Fatalf("update over abandoned lock: %v", err)
	}
	if _, err := os.Stat(lock); !os.IsNotExist(err) {
		t.Fatalf("lock should be released, stat err %v", err)
	}
}

func TestUpdateErrorLeavesFileUntouched(t *testing.T) {
	path := filepath.Join(t.TempDir(), "remotes.json")
	writeRegistry(t, path, Entry{Label: "kept", URL: "http://a", Password: "pw", HostID: "h1"})
	err := Update(path, func([]Entry) ([]Entry, error) { return nil, fmt.Errorf("refused") })
	if err == nil {
		t.Fatal("expected fn error")
	}
	entries, readErr := ReadEntries(path)
	if readErr != nil || len(entries) != 1 {
		t.Fatalf("file changed after refused update: %+v, %v", entries, readErr)
	}
}

// TestRegistryWriterHelper is the body of the child process started by
// TestConcurrentProcessWritesKeepRegistryParseable. It is a no-op otherwise.
func TestRegistryWriterHelper(t *testing.T) {
	path := os.Getenv(writerHelperEnv)
	if path == "" {
		t.Skip("helper process only")
	}
	label := os.Getenv("AO_TEST_REGISTRY_WRITER_LABEL")
	for i := range 40 {
		id := fmt.Sprintf("%s-%d", label, i)
		err := Update(path, func(entries []Entry) ([]Entry, error) {
			return append(entries, Entry{Label: id, URL: "http://" + id, Password: "pw", HostID: id}), nil
		})
		if err != nil {
			t.Fatalf("writer %s iteration %d: %v", label, i, err)
		}
	}
}

func TestConcurrentProcessWritesKeepRegistryParseable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "remotes.json")
	writeRegistry(t, path)

	var children []*exec.Cmd
	for _, label := range []string{"a", "b"} {
		cmd := exec.Command(os.Args[0], "-test.run=^TestRegistryWriterHelper$")
		cmd.Env = append(os.Environ(), writerHelperEnv+"="+path, "AO_TEST_REGISTRY_WRITER_LABEL="+label)
		cmd.Stdout = io.Discard
		cmd.Stderr = io.Discard
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		children = append(children, cmd)
	}
	for _, cmd := range children {
		if err := cmd.Wait(); err != nil {
			t.Fatalf("writer process failed: %v", err)
		}
	}
	entries, err := ReadEntries(path)
	if err != nil {
		t.Fatalf("registry unparseable after concurrent writes: %v", err)
	}
	if len(entries) != 80 {
		t.Fatalf("lost writes: got %d entries, want 80", len(entries))
	}
}
