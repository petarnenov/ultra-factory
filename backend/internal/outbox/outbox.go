// Package outbox is a local, file-based queue for messages the ao CLI could not
// hand to the daemon because no daemon accepted the connection: worker reports
// (`ao report`) and agent activity hooks. The CLI writes one JSON file per
// message; the daemon delivers the queue through its own API router when it
// starts and periodically while it runs, so a report sent while the daemon is
// restarting is not lost.
//
// A message is queued only when the CLI knows it never reached a daemon, so
// delivery from the queue cannot apply it a second time.
package outbox

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Version is the entry format version.
const Version = 1

// Kinds of queued messages.
const (
	KindReport   = "report"
	KindActivity = "activity"
)

// maxEntryBytes bounds a queued file; reports are capped at 1000 characters and
// activity payloads are small.
const maxEntryBytes = 1 << 20

var (
	sessionIDPattern    = regexp.MustCompile(`^[A-Za-z0-9_-]+(?:\.[A-Za-z0-9_-]+)*$`)
	activityPathPattern = regexp.MustCompile(`^sessions/([A-Za-z0-9_.-]+)/activity$`)
)

// Entry is one queued API request.
type Entry struct {
	Version int    `json:"version"`
	Kind    string `json:"kind"`
	// Method and Path are the request the CLI would have sent; Path is below
	// /api/v1/.
	Method    string          `json:"method"`
	Path      string          `json:"path"`
	Body      json.RawMessage `json:"body"`
	SessionID string          `json:"sessionId"`
	CreatedAt time.Time       `json:"createdAt"`
	// ObservedAt is when an activity hook saw its event.
	ObservedAt time.Time `json:"observedAt,omitempty"`
}

// Validate accepts only the requests the CLI queues, so a file planted in an
// outbox directory cannot drive arbitrary API calls.
func (e Entry) Validate() error {
	if e.Version != Version {
		return fmt.Errorf("unsupported outbox entry version %d", e.Version)
	}
	if e.Method != http.MethodPost {
		return fmt.Errorf("unsupported outbox method %q", e.Method)
	}
	if !sessionIDPattern.MatchString(e.SessionID) {
		return fmt.Errorf("invalid outbox session id %q", e.SessionID)
	}
	if e.CreatedAt.IsZero() {
		return errors.New("outbox entry has no creation time")
	}
	if !json.Valid(e.Body) {
		return errors.New("outbox entry body is not JSON")
	}
	switch e.Kind {
	case KindReport:
		if e.Path != "reports" {
			return fmt.Errorf("invalid report path %q", e.Path)
		}
	case KindActivity:
		m := activityPathPattern.FindStringSubmatch(e.Path)
		if len(m) < 2 || m[1] != e.SessionID {
			return fmt.Errorf("invalid activity path %q", e.Path)
		}
		if e.ObservedAt.IsZero() {
			return errors.New("activity entry has no observation time")
		}
	default:
		return fmt.Errorf("unsupported outbox kind %q", e.Kind)
	}
	return nil
}

// Item is a queued entry and the file that holds it.
type Item struct {
	Path  string
	Entry Entry
}

// Rejected is a file in an outbox directory that is not a valid entry.
type Rejected struct {
	Path string
	Err  error
}

// Dirs lists the outbox directories in the order the CLI tries them: the AO
// data directory, then a per-user directory under the temporary directory for
// agent sandboxes where the data directory is read-only. The daemon drains
// both.
func Dirs(dataDir string) []string {
	var dirs []string
	if dataDir != "" {
		dirs = append(dirs, filepath.Join(dataDir, "outbox"))
	}
	name := "ao-outbox"
	if uid := os.Getuid(); uid >= 0 {
		name = fmt.Sprintf("ao-outbox-%d", uid)
	}
	return append(dirs, filepath.Join(os.TempDir(), name))
}

// Write queues e in the first of dirs that accepts it and returns the file.
func Write(dirs []string, e Entry) (string, error) {
	if err := e.Validate(); err != nil {
		return "", err
	}
	data, err := json.Marshal(e)
	if err != nil {
		return "", err
	}
	var errs []error
	for _, dir := range dirs {
		path, err := writeIn(dir, e.CreatedAt, data)
		if err == nil {
			return path, nil
		}
		errs = append(errs, err)
	}
	return "", fmt.Errorf("queue message: %w", errors.Join(errs...))
}

func writeIn(dir string, createdAt time.Time, data []byte) (string, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	if err := checkDir(dir); err != nil {
		return "", err
	}
	tmp, err := os.CreateTemp(dir, ".entry-*.tmp")
	if err != nil {
		return "", err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
		return "", err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmp.Name())
		return "", err
	}
	var suffix [4]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		_ = os.Remove(tmp.Name())
		return "", err
	}
	path := filepath.Join(dir, fmt.Sprintf("%020d-%s.json", createdAt.UnixNano(), hex.EncodeToString(suffix[:])))
	if err := os.Rename(tmp.Name(), path); err != nil {
		_ = os.Remove(tmp.Name())
		return "", err
	}
	return path, nil
}

// checkDir refuses a directory that is a symlink, belongs to another user, or
// is open to other users: its files would be delivered with the daemon's
// authority.
func checkDir(dir string) error {
	info, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("outbox %s is not a directory", dir)
	}
	return checkOwner(dir, info)
}

// Pending reads the queued entries of dirs, oldest first. Directories that do
// not exist are skipped; unsafe directories are reported in errs and not read.
// Files that are not valid entries are returned as rejected so the caller can
// remove them.
func Pending(dirs []string) (items []Item, rejected []Rejected, errs []error) {
	for _, dir := range dirs {
		if _, err := os.Lstat(dir); errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err := checkDir(dir); err != nil {
			errs = append(errs, err)
			continue
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		for _, de := range entries {
			name := de.Name()
			if strings.HasPrefix(name, ".") || !strings.HasSuffix(name, ".json") {
				continue
			}
			path := filepath.Join(dir, name)
			entry, err := readEntry(path)
			if err != nil {
				rejected = append(rejected, Rejected{Path: path, Err: err})
				continue
			}
			items = append(items, Item{Path: path, Entry: entry})
		}
	}
	sort.SliceStable(items, func(i, j int) bool {
		if !items[i].Entry.CreatedAt.Equal(items[j].Entry.CreatedAt) {
			return items[i].Entry.CreatedAt.Before(items[j].Entry.CreatedAt)
		}
		return items[i].Path < items[j].Path
	})
	return items, rejected, errs
}

func readEntry(path string) (Entry, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return Entry{}, err
	}
	if !info.Mode().IsRegular() {
		return Entry{}, errors.New("not a regular file")
	}
	f, err := os.Open(path) //nolint:gosec // path is inside a checked outbox directory
	if err != nil {
		return Entry{}, err
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(io.LimitReader(f, maxEntryBytes+1))
	if err != nil {
		return Entry{}, err
	}
	if len(data) > maxEntryBytes {
		return Entry{}, errors.New("entry too large")
	}
	var entry Entry
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&entry); err != nil {
		return Entry{}, err
	}
	if err := entry.Validate(); err != nil {
		return Entry{}, err
	}
	return entry, nil
}

// Remove deletes a delivered or rejected entry.
func Remove(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}
