package domain

import (
	"errors"
	"strings"
	"time"
)

// RemoteWorkerLink records that a worker session on a saved remote AO host
// belongs to a project on this daemon, so this daemon pulls the worker's
// reports and delivers them to the project's active orchestrator. Session and
// project IDs on the remote side are the remote daemon's own.
type RemoteWorkerLink struct {
	HostID          string
	HostLabel       string
	RemoteSessionID SessionID
	RemoteProjectID ProjectID
	ProjectID       ProjectID
	DisplayName     string
	// OwnerSessionID is a local coordinator in ProjectID. Nil means root-owned;
	// the remote parent is never stored because remote IDs are the host's own.
	OwnerSessionID *SessionID
	// Source records how the link was made: by a spawn from this daemon, or
	// by the discovery pass that found a live worker on a saved host.
	Source    LinkSource
	CreatedAt time.Time
}

// LinkSource says how a remote worker link came to exist.
type LinkSource string

const (
	// LinkSourceSpawn is a link written by `ao spawn --host`.
	LinkSourceSpawn LinkSource = "spawn"
	// LinkSourceDiscovered is a link written by remote worker discovery.
	LinkSourceDiscovered LinkSource = "discovered"
)

// ErrInvalidRemoteWorkerLink reports a link missing a required identity.
var ErrInvalidRemoteWorkerLink = errors.New("invalid remote worker link")

// Validate checks the identities the link needs to be polled and delivered.
func (l RemoteWorkerLink) Validate() error {
	if strings.TrimSpace(l.HostID) == "" || strings.TrimSpace(string(l.RemoteSessionID)) == "" ||
		strings.TrimSpace(string(l.RemoteProjectID)) == "" || l.ProjectID == "" || l.CreatedAt.IsZero() {
		return ErrInvalidRemoteWorkerLink
	}
	return nil
}
