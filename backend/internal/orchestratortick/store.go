package orchestratortick

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Sent is the last state a tick delivered to the orchestrator, as its
// Fingerprint.
type Sent struct {
	Session     string    `json:"session"`
	Fingerprint string    `json:"fingerprint"`
	At          time.Time `json:"at"`
}

// Record is the outcome of the last tick, for `ao orchestrator tick status`.
type Record struct {
	At         time.Time `json:"at"`
	Outcome    Outcome   `json:"outcome"`
	Session    string    `json:"session,omitempty"`
	Reason     string    `json:"reason,omitempty"`
	Violations []string  `json:"violations,omitempty"`
}

// Store keeps a tick's state between runs.
type Store interface {
	// Paused reports the maintenance flag and the reason it was set with.
	Paused() (reason string, paused bool, err error)
	Pause(reason string) error
	// Resume clears the flag and reports whether it was set.
	Resume() (bool, error)
	LastSent() (Sent, bool, error)
	SaveSent(Sent) error
	LastTick() (Record, bool, error)
	SaveTick(Record) error
}

// FileStore keeps the state as files in Dir (mode 0700), each replaced
// atomically so a crash never leaves a partial file.
type FileStore struct{ Dir string }

const (
	pausedFile   = "paused"
	lastSentFile = "last-sent.json"
	lastTickFile = "last-tick.json"
)

// Paused implements Store.
func (s FileStore) Paused() (string, bool, error) {
	b, err := os.ReadFile(filepath.Join(s.Dir, pausedFile))
	if errors.Is(err, os.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return strings.TrimSpace(string(b)), true, nil
}

// Pause implements Store.
func (s FileStore) Pause(reason string) error {
	return s.write(pausedFile, []byte(reason+"\n"))
}

// Resume implements Store.
func (s FileStore) Resume() (bool, error) {
	err := os.Remove(filepath.Join(s.Dir, pausedFile))
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return err == nil, err
}

// LastSent implements Store.
func (s FileStore) LastSent() (Sent, bool, error) {
	var v Sent
	ok, err := s.readJSON(lastSentFile, &v)
	return v, ok, err
}

// SaveSent implements Store.
func (s FileStore) SaveSent(v Sent) error { return s.writeJSON(lastSentFile, v) }

// LastTick implements Store.
func (s FileStore) LastTick() (Record, bool, error) {
	var v Record
	ok, err := s.readJSON(lastTickFile, &v)
	return v, ok, err
}

// SaveTick implements Store.
func (s FileStore) SaveTick(v Record) error { return s.writeJSON(lastTickFile, v) }

func (s FileStore) readJSON(name string, v any) (bool, error) {
	b, err := os.ReadFile(filepath.Join(s.Dir, name))
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if err := json.Unmarshal(b, v); err != nil {
		return false, fmt.Errorf("read %s: %w", name, err)
	}
	return true, nil
}

func (s FileStore) writeJSON(name string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return s.write(name, append(b, '\n'))
}

func (s FileStore) write(name string, data []byte) error {
	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(s.Dir, "."+name+".*.tmp")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmp.Name())
		return err
	}
	if err := os.Rename(tmp.Name(), filepath.Join(s.Dir, name)); err != nil {
		_ = os.Remove(tmp.Name())
		return err
	}
	return nil
}
