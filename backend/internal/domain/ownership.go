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

// CoordinatorLimitEvent audits one worker-limit change. Origin is "root" or
// the operator origin acting for the live root.
type CoordinatorLimitEvent struct {
	ID             int64
	SessionID      SessionID
	PreviousLimit  *int
	NewLimit       int
	ActorSessionID SessionID
	Origin         string
	CreatedAt      time.Time
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
