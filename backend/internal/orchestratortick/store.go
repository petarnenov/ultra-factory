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
	At          time.Time `json:"at"`
	Outcome     Outcome   `json:"outcome"`
	Session     string    `json:"session,omitempty"`
	Reason      string    `json:"reason,omitempty"`
	Violations  []string  `json:"violations,omitempty"`
	Completions []string  `json:"completions,omitempty"`
}

// Obligation is one durable reason to keep a worker open after its owned PRs
// reached a terminal state. Completing it means removing the obligation.
type Obligation struct {
	ID                  string    `json:"id"`
	Reason              string    `json:"reason"`
	CompletionCondition string    `json:"completionCondition"`
	CreatedAt           time.Time `json:"createdAt"`
}

// CompletionAction is one idempotent provider or lifecycle mutation. The key
// pins the action to the facts it was evaluated against, so a repeated tick
// can verify or retry it without repeating a completed side effect.
type CompletionAction struct {
	Kind           string    `json:"kind"`
	Status         string    `json:"status"`
	PR             int       `json:"pr,omitempty"`
	HeadSHA        string    `json:"headSha,omitempty"`
	MergeCommitSHA string    `json:"mergeCommitSha,omitempty"`
	Attempts       int       `json:"attempts,omitempty"`
	LastError      string    `json:"lastError,omitempty"`
	UpdatedAt      time.Time `json:"updatedAt"`
}

// Action statuses.
const (
	CompletionActionPending   = "pending"
	CompletionActionCompleted = "completed"
	CompletionActionFailed    = "failed"
)

// SessionCompletion is the durable completion state of one host-qualified
// worker. Remaining is the last reconciled explanation; it is not a derived
// display status and never substitutes for the underlying PR, branch, or
// activity facts.
type SessionCompletion struct {
	Host        string                      `json:"host"`
	Session     string                      `json:"session"`
	Obligations []Obligation                `json:"obligations,omitempty"`
	Actions     map[string]CompletionAction `json:"actions,omitempty"`
	Remaining   []string                    `json:"remaining,omitempty"`
	UpdatedAt   time.Time                   `json:"updatedAt"`
}

// CompletionState is the durable completion reconciliation state.
type CompletionState struct {
	Sessions map[string]SessionCompletion `json:"sessions,omitempty"`
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
	Completion() (CompletionState, bool, error)
	SaveCompletion(CompletionState) error
}

// FileStore keeps the state as files in Dir (mode 0700), each replaced
// atomically so a crash never leaves a partial file.
type FileStore struct{ Dir string }

const (
	pausedFile     = "paused"
	lastSentFile   = "last-sent.json"
	lastTickFile   = "last-tick.json"
	completionFile = "completion.json"
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

// Completion implements Store.
func (s FileStore) Completion() (CompletionState, bool, error) {
	var v CompletionState
	ok, err := s.readJSON(completionFile, &v)
	if v.Sessions == nil {
		v.Sessions = map[string]SessionCompletion{}
	}
	return v, ok, err
}

// SaveCompletion implements Store.
func (s FileStore) SaveCompletion(v CompletionState) error {
	if v.Sessions == nil {
		v.Sessions = map[string]SessionCompletion{}
	}
	return s.writeJSON(completionFile, v)
}

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
