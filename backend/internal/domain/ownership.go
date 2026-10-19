package domain

import "time"

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

// Valid reports whether r is one of the bounded ownership event reasons.
func (r OwnershipEventReason) Valid() bool {
	switch r {
	case OwnershipSpawnParent, OwnershipExplicitAssignment, OwnershipExplicitReassignment,
		OwnershipCoordinatorTerminated, OwnershipOwnerNotLiveReconciled:
		return true
	}
	return false
}
