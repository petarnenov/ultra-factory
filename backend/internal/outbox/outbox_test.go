package outbox

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func report(at time.Time) Entry {
	return Entry{Version: Version, Kind: KindReport, Method: "POST", Path: "reports",
		Body: json.RawMessage(`{"sessionId":"ao-1","message":"hi"}`), SessionID: "ao-1", CreatedAt: at}
}

func TestWriteThenPendingOldestFirst(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "outbox")
	t0 := time.Unix(1000, 0).UTC()
	for _, at := range []time.Time{t0.Add(2 * time.Second), t0, t0.Add(time.Second)} {
		if _, err := Write([]string{dir}, report(at)); err != nil {
			t.Fatal(err)
		}
	}
	items, rejected, errs := Pending([]string{dir})
	if len(rejected) != 0 || len(errs) != 0 {
		t.Fatalf("rejected=%v errs=%v", rejected, errs)
	}
	if len(items) != 3 {
		t.Fatalf("items = %d, want 3", len(items))
	}
	for i, want := range []time.Time{t0, t0.Add(time.Second), t0.Add(2 * time.Second)} {
		if !items[i].Entry.CreatedAt.Equal(want) {
			t.Fatalf("item %d createdAt = %v, want %v", i, items[i].Entry.CreatedAt, want)
		}
	}
	if err := Remove(items[0].Path); err != nil {
		t.Fatal(err)
	}
	if items, _, _ := Pending([]string{dir}); len(items) != 2 {
		t.Fatalf("after remove items = %d, want 2", len(items))
	}
}

func TestWriteFallsBackToTheNextDirectory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("read-only directories are not enforced the same way on Windows")
	}
	root := t.TempDir()
	readOnly := filepath.Join(root, "ro")
	if err := os.Mkdir(readOnly, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(readOnly, 0o700) })
	fallback := filepath.Join(root, "fallback")
	path, err := Write([]string{filepath.Join(readOnly, "outbox"), fallback}, report(time.Unix(1, 0)))
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(path) != fallback {
		t.Fatalf("queued at %s, want under %s", path, fallback)
	}
}

func TestUnsafeDirectoriesAreRefused(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permissions")
	}
	root := t.TempDir()
	open := filepath.Join(root, "open")
	if err := os.Mkdir(open, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(open, 0o777); err != nil {
		t.Fatal(err)
	}
	if _, err := Write([]string{open}, report(time.Unix(1, 0))); err == nil {
		t.Fatal("wrote into a world-writable outbox")
	}
	target := filepath.Join(root, "target")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if _, err := Write([]string{link}, report(time.Unix(1, 0))); err == nil {
		t.Fatal("wrote through a symlinked outbox")
	}
	if _, _, errs := Pending([]string{open, link}); len(errs) != 2 {
		t.Fatalf("Pending errs = %v, want both directories refused", errs)
	}
}

func TestValidateAcceptsOnlyQueuedRequests(t *testing.T) {
	at := time.Unix(1, 0)
	activity := Entry{Version: Version, Kind: KindActivity, Method: "POST", Path: "sessions/ao-1/activity",
		Body: json.RawMessage(`{}`), SessionID: "ao-1", CreatedAt: at, ObservedAt: at}
	if err := activity.Validate(); err != nil {
		t.Fatalf("valid activity: %v", err)
	}
	for name, mutate := range map[string]func(*Entry){
		"other path":             func(e *Entry) { e.Path = "sessions/ao-1/kill" },
		"other session's path":   func(e *Entry) { e.Path = "sessions/ao-2/activity" },
		"other method":           func(e *Entry) { e.Method = "DELETE" },
		"unknown kind":           func(e *Entry) { e.Kind = "send" },
		"no observation time":    func(e *Entry) { e.ObservedAt = time.Time{} },
		"bad session id":         func(e *Entry) { e.SessionID = "../x"; e.Path = "sessions/../x/activity" },
		"body is not JSON":       func(e *Entry) { e.Body = json.RawMessage(`{`) },
		"unsupported version":    func(e *Entry) { e.Version = 2 },
		"report with other path": func(e *Entry) { e.Kind = KindReport; e.Path = "sessions/ao-1/activity" },
	} {
		e := activity
		mutate(&e)
		if err := e.Validate(); err == nil {
			t.Errorf("%s: Validate accepted %+v", name, e)
		}
	}
}

func TestPendingRejectsPlantedFiles(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "outbox")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	planted := Entry{Version: Version, Kind: KindReport, Method: "POST", Path: "projects",
		Body: json.RawMessage(`{}`), SessionID: "ao-1", CreatedAt: time.Unix(1, 0)}
	data, _ := json.Marshal(planted)
	if err := os.WriteFile(filepath.Join(dir, "1-planted.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	items, rejected, _ := Pending([]string{dir})
	if len(items) != 0 || len(rejected) != 1 {
		t.Fatalf("items=%d rejected=%d, want the planted file rejected", len(items), len(rejected))
	}
}
