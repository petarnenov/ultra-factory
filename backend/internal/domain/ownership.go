package domain

import (
	"strings"
	"time"
	"unicode"
)

// SessionOwnerEvent is one append-only audit row for a local worker's
// ownership transition; owner values stay plain IDs after an owner is removed.
type SessionOwnerEvent struct {
	ID                     int64
	SessionID              SessionID
	PreviousOwnerSessionID *SessionID
	NewOwnerSessionID      *SessionID
	Reason                 OwnershipEventReason
	CreatedAt              time.Time
}

// HostSlot is one coordinator's worker slots on a paired remote host.
type HostSlot struct {
	HostID string `json:"hostId"`
	Slots  int    `json:"slots"`
}

// CoordinatorLimitEvent audits a worker-limit or host-slot change. Limits
// are stored values (nil: project value); the actor is the live root.
type CoordinatorLimitEvent struct {
	ID                int64
	SessionID         SessionID
	PreviousLimit     *int
	NewLimit          *int
	PreviousHostSlots []HostSlot
	NewHostSlots      []HostSlot
	ActorSessionID    SessionID
	Origin            string
	CreatedAt         time.Time
}

// CoordinatorOperatorEvent audits a coordinator spawned by an operator on
// behalf of the live root. Origin is ui, cli or api.
type CoordinatorOperatorEvent struct {
	CoordinatorSessionID SessionID
	ProjectID            ProjectID
	RootSessionID        SessionID
	Origin               string
	Scope                string
	WorkerLimit          *int
	HostSlots            []HostSlot
	CoordinatorAgent     *AgentSelection
	WorkerAgent          *AgentSelection
	CreatedAt            time.Time
}

// RemoteWorkerOwnerEvent is one append-only audit row for a remote worker
// link's ownership transition, keyed by the link's durable identity.
type RemoteWorkerOwnerEvent struct {
	ID                     int64
	HostID                 string
	RemoteSessionID        SessionID
	PreviousOwnerSessionID *SessionID
	NewOwnerSessionID      *SessionID
	Reason                 OwnershipEventReason
	CreatedAt              time.Time
}

// NormalizeCoordinatorScope strips control characters and trims a scope.
// The SQLite triggers reject unnormalized values from direct writers.
func NormalizeCoordinatorScope(scope string) string {
	stripped := strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, scope)
	return strings.TrimSpace(stripped)
}

// Valid reports whether r is one of the bounded ownership event reasons.
func (r OwnershipEventReason) Valid() bool {
	switch r {
	case OwnershipSpawnParent, OwnershipExplicitAssignment, OwnershipExplicitReassignment,
		OwnershipCoordinatorTerminated, OwnershipOwnerNotLiveReconciled:
		return true
	}
	return false
}
