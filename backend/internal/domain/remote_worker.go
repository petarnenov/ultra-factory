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
	// LastSeenAt is the last time the host answered a read of this worker.
	// Nil means no read has succeeded yet; created time stands in for it.
	LastSeenAt *time.Time
	// LastState is the worker state of the last successful read: 'live',
	// 'terminated', or empty before the first read.
	LastState string
}

// RemoteWorkerLinkStaleAfter is how long an unreachable host's link keeps
// counting toward capacity after its last successful read.
const RemoteWorkerLinkStaleAfter = 10 * time.Minute

// Remote worker link states as shown by `ao remote-worker ls`.
const (
	RemoteLinkLive       = "live"
	RemoteLinkStale      = "stale"
	RemoteLinkTerminated = "terminated"
)

// State derives the link state at now. A terminated read wins; otherwise a
// link whose host has not answered within the grace period is stale.
func (l RemoteWorkerLink) State(now time.Time) string {
	if l.LastState == RemoteLinkTerminated {
		return RemoteLinkTerminated
	}
	seen := l.CreatedAt
	if l.LastSeenAt != nil {
		seen = *l.LastSeenAt
	}
	if now.Sub(seen) > RemoteWorkerLinkStaleAfter {
		return RemoteLinkStale
	}
	return RemoteLinkLive
}

// LinkSource says how a remote worker link came to exist.
type LinkSource string

const (
	// LinkSourceSpawn is a link written by `ao spawn --host`.
	LinkSourceSpawn LinkSource = "spawn"
	// LinkSourceDiscovered is a link written by remote worker discovery.
	LinkSourceDiscovered LinkSource = "discovered"
)

// RemoteWorkerLinkRemoval remembers a link an operator removed by hand.
type RemoteWorkerLinkRemoval struct {
	HostID          string
	RemoteSessionID SessionID
	RemovedAt       time.Time
}

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
