package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

// CoordinatorSummaryState is the coordinator's own assertion about its scope.
type CoordinatorSummaryState string

// Supported coordinator summary states.
const (
	SummaryPlanning CoordinatorSummaryState = "planning"
	SummaryWorking  CoordinatorSummaryState = "working"
	SummaryBlocked  CoordinatorSummaryState = "blocked"
	SummaryDone     CoordinatorSummaryState = "done"
)

// EscalationReason names why a coordinator needs the root's attention.
type EscalationReason string

// Supported escalation reasons.
const (
	EscalationOverlappingFiles        EscalationReason = "overlapping_files"
	EscalationUnclearAcceptance       EscalationReason = "unclear_acceptance"
	EscalationWorkerFailed            EscalationReason = "worker_failed"
	EscalationScopeDecision           EscalationReason = "scope_decision"
	EscalationCoordinatorTerminated   EscalationReason = "coordinator_terminated"
	EscalationOther                   EscalationReason = "other"
	maxSummaryScopeCharacters                          = 200
	maxSummaryResultCharacters                         = 2000
	maxSummaryEscalationNoteChars                      = 500
	maxSummarySubmissionKeyCharacters                  = 200
	// MaxSummaryWorkers bounds the workers one summary links to.
	MaxSummaryWorkers = 3
	// MaxSummaryReadyPRs bounds the ready PRs one summary hands to the root.
	MaxSummaryReadyPRs          = 10
	maxReadyPRHeadSHACharacters = 64
)

// DaemonSubmissionKeyPrefix is reserved for keys the daemon writes itself.
const DaemonSubmissionKeyPrefix = "daemon:"

// CoordinatorSummaryBatchWindow is how long a non-escalation summary waits so
// a chatty coordinator's updates reach the root as one turn.
const CoordinatorSummaryBatchWindow = 30 * time.Second

// ErrInvalidCoordinatorSummary reports a payload outside the bounds.
var ErrInvalidCoordinatorSummary = errors.New("invalid coordinator summary")

// SummaryRemoteWorker names a remote worker link a summary covers.
type SummaryRemoteWorker struct {
	HostID          string
	HostLabel       string
	RemoteSessionID string
}

// ReadyPR is a pull request a coordinator hands to the root as ready to
// merge: HeadSHA is the head an AO review run approved.
type ReadyPR struct {
	Number      int
	HeadSHA     string
	ReviewRunID string
}

// CoordinatorSummary is a durable coordinator-to-root assertion. It is a
// distinct record from a worker report and never replaces one.
type CoordinatorSummary struct {
	ID                   string
	CoordinatorSessionID SessionID
	ProjectID            ProjectID
	SubmissionKey        string
	Scope                string
	State                CoordinatorSummaryState
	WorkerSessionIDs     []SessionID
	// RemoteWorkers records linked workers covered by this summary.
	RemoteWorkers    []SummaryRemoteWorker
	ReadyPRs         []ReadyPR
	Result           string
	EscalationReason EscalationReason
	EscalationNote   string
	CreatedAt        time.Time
	AvailableAt      time.Time
	DeliveryBatchID  string
	DeliveryOwnerID  string
	DeliveryState    ReportDeliveryState
	ClaimToken       string
	ClaimedAt        time.Time
	DeliveryAttempts int64
	AcknowledgedAt   time.Time
	LastError        string
	// CoordinatorDisplayName is resolved at delivery time for presentation.
	CoordinatorDisplayName string
}

// IsEscalation reports a blocked summary that asks for the root's attention.
func (s CoordinatorSummary) IsEscalation() bool {
	return s.State == SummaryBlocked && s.EscalationReason != ""
}

// ValidateCoordinatorSummary checks the section 6 payload bounds. Worker
// ownership is a service concern because it needs live session state.
func ValidateCoordinatorSummary(s CoordinatorSummary) error {
	if strings.TrimSpace(s.SubmissionKey) == "" || utf8.RuneCountInString(s.SubmissionKey) > maxSummarySubmissionKeyCharacters {
		return ErrInvalidCoordinatorSummary
	}
	if strings.TrimSpace(s.Scope) == "" || utf8.RuneCountInString(s.Scope) > maxSummaryScopeCharacters {
		return ErrInvalidCoordinatorSummary
	}
	switch s.State {
	case SummaryPlanning, SummaryWorking, SummaryBlocked, SummaryDone:
	default:
		return ErrInvalidCoordinatorSummary
	}
	if len(s.WorkerSessionIDs) > MaxSummaryWorkers {
		return ErrInvalidCoordinatorSummary
	}
	seen := map[SessionID]bool{}
	for _, id := range s.WorkerSessionIDs {
		if id == "" || seen[id] {
			return ErrInvalidCoordinatorSummary
		}
		seen[id] = true
	}
	if len(s.ReadyPRs) > MaxSummaryReadyPRs {
		return ErrInvalidCoordinatorSummary
	}
	readyNumbers := map[int]bool{}
	for _, pr := range s.ReadyPRs {
		if pr.Number <= 0 || readyNumbers[pr.Number] || strings.TrimSpace(pr.ReviewRunID) == "" ||
			strings.TrimSpace(pr.HeadSHA) == "" || utf8.RuneCountInString(pr.HeadSHA) > maxReadyPRHeadSHACharacters {
			return ErrInvalidCoordinatorSummary
		}
		readyNumbers[pr.Number] = true
	}
	if utf8.RuneCountInString(s.Result) > maxSummaryResultCharacters {
		return ErrInvalidCoordinatorSummary
	}
	switch s.EscalationReason {
	case "":
		if s.EscalationNote != "" {
			return ErrInvalidCoordinatorSummary
		}
	case EscalationOverlappingFiles, EscalationUnclearAcceptance, EscalationWorkerFailed,
		EscalationScopeDecision, EscalationCoordinatorTerminated, EscalationOther:
		if s.State != SummaryBlocked {
			return ErrInvalidCoordinatorSummary
		}
		if s.EscalationReason == EscalationOther && strings.TrimSpace(s.EscalationNote) == "" {
			return ErrInvalidCoordinatorSummary
		}
	default:
		return ErrInvalidCoordinatorSummary
	}
	if utf8.RuneCountInString(s.EscalationNote) > maxSummaryEscalationNoteChars {
		return ErrInvalidCoordinatorSummary
	}
	return nil
}

// CoordinatorSummaryBatchID is the stable identity of one summary set:
// coordinator-summary:<id>, or a hash of the sorted ids for a coalesced set.
func CoordinatorSummaryBatchID(ids []string) string {
	if len(ids) == 1 {
		return "coordinator-summary:" + ids[0]
	}
	sorted := append([]string(nil), ids...)
	sort.Strings(sorted)
	sum := sha256.Sum256([]byte(strings.Join(sorted, "\x00")))
	return "coordinator-summary-batch:" + hex.EncodeToString(sum[:16])
}
