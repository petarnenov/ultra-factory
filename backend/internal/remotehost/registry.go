package remotehost

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"time"
)

// lockWait bounds how long a writer waits for another writer's lock.
var lockWait = 10 * time.Second

// lockStale is the age after which a lock file is assumed abandoned by a
// crashed writer. Every critical section is a read and a rename, far shorter.
const lockStale = 30 * time.Second

// lockPollInterval is how often a waiting writer retries the lock.
const lockPollInterval = 10 * time.Millisecond

// LockPath is the lock file that serializes writers of path across processes.
// The desktop app (frontend/src/main/remotes-store.ts) uses the same name and
// protocol, so both sides exclude each other.
func LockPath(path string) string {
	return path + ".lock"
}

// Update runs fn against the saved hosts at path under the cross-process lock
// and writes the result back atomically with mode 0600. A missing file starts
// from an empty list. An error from fn leaves the file untouched.
func Update(path string, fn func([]Entry) ([]Entry, error)) error {
	return withLock(path, func() error {
		entries, err := ReadEntries(path)
		if err != nil {
			return err
		}
		next, err := fn(entries)
		if err != nil {
			return err
		}
		return saveEntries(path, next)
	})
}

// withLock holds the lock file for path while fn runs. It creates the lock
// exclusively, removes it afterwards, and breaks a lock older than lockStale.
func withLock(path string, fn func() error) error {
	lock := LockPath(path)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	deadline := time.Now().Add(lockWait)
	for {
		file, err := os.OpenFile(lock, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600) // #nosec G304 -- AO-owned lock file.
		if err == nil {
			_ = file.Close()
			defer func() { _ = os.Remove(lock) }()
			return fn()
		}
		if !errors.Is(err, fs.ErrExist) {
			return err
		}
		if info, statErr := os.Stat(lock); statErr == nil && time.Since(info.ModTime()) > lockStale {
			_ = os.Remove(lock)
			continue
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timed out waiting for %s", lock)
		}
		time.Sleep(lockPollInterval)
	}
}

// writeEntries replaces path with entries. The temporary file is 0600 before
// its contents are written, so a crash never truncates the only saved copy.
func saveEntries(path string, entries []Entry) error {
	if entries == nil {
		entries = []Entry{}
	}
	raw, err := json.MarshalIndent(struct {
		Remotes []Entry `json:"remotes"`
	}{Remotes: entries}, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	temporary := fmt.Sprintf("%s.%d.tmp", path, os.Getpid())
	file, err := os.OpenFile(temporary, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600) // #nosec G304 -- AO-owned temp file.
	if err != nil {
		return err
	}
	_, werr := file.Write(append(raw, '\n'))
	cerr := file.Close()
	if err := errors.Join(werr, cerr); err != nil {
		_ = os.Remove(temporary)
		return err
	}
	if err := os.Rename(temporary, path); err != nil {
		_ = os.Remove(temporary)
		return err
	}
	return nil
}

// MigrationReport says what MigrateInstanceFile did, for the daemon's log.
type MigrationReport struct {
	// Moved is true when the instance-local file became the shared file.
	Moved bool
	// Merged lists the labels of instance-local hosts added to the shared file.
	Merged []string
}

// MigrateInstanceFile folds an instance-local saved-hosts file into the shared
// one. It runs once per daemon start. When only the local file exists it is
// moved to the shared path. When both exist, hosts are matched by host ID (by
// URL when unpaired), the shared entry wins, and local-only hosts are added.
// The local file is removed once its hosts are in the shared file.
func MigrateInstanceFile(local, shared string, log *slog.Logger) (MigrationReport, error) {
	var report MigrationReport
	if filepath.Clean(local) == filepath.Clean(shared) {
		return report, nil
	}
	if _, err := os.Stat(local); errors.Is(err, fs.ErrNotExist) {
		return report, nil
	} else if err != nil {
		return report, err
	}
	localEntries, err := ReadEntries(local)
	if err != nil {
		return report, err
	}
	_, statErr := os.Stat(shared)
	sharedMissing := errors.Is(statErr, fs.ErrNotExist)
	err = Update(shared, func(sharedEntries []Entry) ([]Entry, error) {
		merged := append([]Entry(nil), sharedEntries...)
		for _, entry := range localEntries {
			if containsHost(sharedEntries, entry) {
				continue
			}
			merged = append(merged, entry)
			report.Merged = append(report.Merged, entry.Label)
		}
		return merged, nil
	})
	if err != nil {
		return report, err
	}
	if sharedMissing {
		report.Moved = true
		log.Info("moved instance-local saved hosts to the shared registry", "from", local, "to", shared, "hosts", len(localEntries))
	} else if len(report.Merged) > 0 {
		log.Info("merged instance-local saved hosts into the shared registry", "from", local, "to", shared, "added", report.Merged)
	}
	if err := os.Remove(local); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return report, err
	}
	return report, nil
}

// containsHost reports whether entries already hold the same host as entry:
// the same host ID when paired, otherwise the same URL.
func containsHost(entries []Entry, entry Entry) bool {
	for _, candidate := range entries {
		if entry.HostID != "" && candidate.HostID == entry.HostID {
			return true
		}
		if entry.HostID == "" && candidate.URL == entry.URL {
			return true
		}
	}
	return false
}
