//go:build windows

package persistenthost

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

func TestReadHostFileWaitsOutAnExclusiveHolder(t *testing.T) {
	path := filepath.Join(t.TempDir(), "host.json")
	want := []byte(`{"ok":true}`)
	if err := os.WriteFile(path, want, 0o600); err != nil {
		t.Fatal(err)
	}
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		t.Fatal(err)
	}
	// No share mode: any other open of the file fails with a sharing violation
	// until the handle is closed, as when a scanner holds a new file.
	holder, err := windows.CreateFile(name, windows.GENERIC_READ, 0, nil,
		windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		t.Fatal(err)
	}
	time.AfterFunc(100*time.Millisecond, func() { _ = windows.CloseHandle(holder) })
	got, err := readHostFile(path)
	if err != nil {
		t.Fatalf("readHostFile: %v", err)
	}
	if string(got) != string(want) {
		t.Fatalf("contents = %q, want %q", got, want)
	}
}
