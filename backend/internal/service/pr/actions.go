package pr

import (
	"context"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

// ActionManager is the controller-facing contract for guarded PR mutations.
type ActionManager interface {
	Merge(ctx context.Context, request MergeRequest) (MergeResult, error)
	ResolveComments(ctx context.Context, prID string, commentIDs []string) (ResolveResult, error)
	SetReady(ctx context.Context, request ReadyRequest) (ReadyResult, error)
}

// ReadyRequest marks a session's open PR ready for review, or back to draft.
type ReadyRequest struct {
	SessionID domain.SessionID
	// Ready is true to mark ready for review, false to convert to draft.
	Ready bool
	// RequestedBySessionID names the calling session. Empty is an anonymous
	// caller (a person or a remote host) and skips the role check.
	RequestedBySessionID domain.SessionID
}

// ReadyResult reports the PR a ready request acted on.
type ReadyResult struct {
	PRURL    string
	PRNumber int
	HeadSHA  string
	Ready    bool
	// Changed is false when the PR already had the requested state.
	Changed bool
}

// MergeRequest identifies the tracked PR and pins the mutation to the exact
// head the user saw when they clicked Merge.
type MergeRequest struct {
	PRID            string
	PRURL           string
	ExpectedHeadSHA string
	// RequestedBySessionID optionally names the session asking for the merge.
	// Empty keeps the caller anonymous and skips the merge-authority check.
	RequestedBySessionID domain.SessionID
}

// MergeResult is the successful outcome of a PR merge.
type MergeResult struct {
	PRNumber       int
	Method         string
	MergeCommitSHA string
}

// ResolveResult is the successful outcome of a resolve-comments operation.
type ResolveResult struct {
	Resolved int
}
