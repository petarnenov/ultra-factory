package orchestratortick

import (
	"context"
	"time"
)

// WorkerPR is one pull request attributed to a worker session.
type WorkerPR struct {
	Number int
	// State is the provider lifecycle state as the daemon last observed it:
	// open, merged, closed, or empty when unknown.
	State string
}

// WorkerBranchState is the session branch's durable push state. Known is
// false when the daemon has not observed the branch yet; Unpushed commits are
// work that only exists in the workspace.
type WorkerBranchState struct {
	Known    bool
	Commits  int
	Unpushed int
}

// WorkerReview is AO's current-head native review state for one worker PR.
type WorkerReview struct {
	PRNumber  int
	TargetSHA string
	Status    string
}

// Worker is one AO session on a host, as a tick needs it.
type Worker struct {
	ID         string
	Kind       string
	Branch     string
	Activity   string
	Harness    string
	Terminated bool
	// PRNumber and CI describe the session's first pull request; PRNumber 0
	// means it has none.
	PRNumber       int
	CI             string
	PRs            []WorkerPR
	Reviews        []WorkerReview
	BranchState    *WorkerBranchState
	LastActivityAt time.Time
	CreatedAt      time.Time
}

// HostSessions are the worker sessions of one host, or why they are unknown.
type HostSessions struct {
	Host     string
	Sessions []Worker
	Err      error
}

// Check is one status check of a pull request's head commit.
type Check struct {
	// Result is the check's conclusion, or its status while it runs, in
	// GitHub's upper-case form (SUCCESS, FAILURE, IN_PROGRESS, ...).
	Result      string
	CompletedAt time.Time
}

// ReviewThread is one provider review thread on a pull request.
type ReviewThread struct {
	Resolved bool
	Outdated bool
	Author   string
	IsBot    bool
	Body     string
}

// PullRequest is an open pull request to the base branch.
type PullRequest struct {
	Number         int
	Head           string
	HeadSHA        string
	Title          string
	Body           string
	Labels         []string
	Draft          bool
	Mergeable      string
	ReviewDecision string
	Threads        []ReviewThread
	// AOReviewStatus is AO's native review state for this exact head, set by
	// the cadence from the worker's review read model. Empty means AO has no
	// recorded decision for this head.
	AOReviewStatus string
	Checks         []Check
	UpdatedAt      time.Time
}

// MergedPR is a pull request already merged into the base branch.
type MergedPR struct {
	Number   int
	Head     string
	HeadSHA  string
	Body     string
	MergedAt time.Time
}

// ClosedPR is a pull request closed without merging.
type ClosedPR struct {
	Number   int
	Head     string
	HeadSHA  string
	ClosedAt time.Time
}

// GitHubState is what a tick reads from GitHub.
type GitHubState struct {
	OpenPRs   []PullRequest
	MergedPRs []MergedPR
	ClosedPRs []ClosedPR
	// OpenIssues maps the open backlog issues to their titles.
	OpenIssues map[int]string
	// BehindMain counts the main commits the base branch lacks; BehindSHAs
	// holds the short SHAs of the newest of them.
	BehindMain int
	BehindSHAs []string
}

// DaemonStatus is the local daemon's state; Ready means it can serve a tick.
type DaemonStatus struct {
	Ready  bool
	Detail string
}

// Orchestrator is a live orchestrator session.
type Orchestrator struct {
	ID        string
	ProjectID string
}

// SessionState is what a tick needs to know about the orchestrator.
type SessionState struct {
	Status     string
	Activity   string
	Terminated bool
}

// Daemon is the local AO daemon and, through it, the AO hosts.
type Daemon interface {
	Status(ctx context.Context) (DaemonStatus, error)
	// Orchestrators lists the live (not terminated) orchestrator sessions.
	Orchestrators(ctx context.Context) ([]Orchestrator, error)
	Session(ctx context.Context, id string) (SessionState, error)
	Workers(ctx context.Context, host string) ([]Worker, error)
	Send(ctx context.Context, sessionID, message string) error
	// Kill terminates one host-qualified worker through the existing session
	// lifecycle and verifies the resulting terminated fact. It never cleans or
	// deletes the workspace.
	Kill(ctx context.Context, host, sessionID string) error
}

// Merger applies one provider merge guarded by the reviewed head SHA.
type Merger interface {
	Merge(ctx context.Context, request MergeRequest) (MergeResult, error)
}

// MergeRequest pins a merge to the exact head the reconciler evaluated.
type MergeRequest struct {
	Repo            string
	Number          int
	ExpectedHeadSHA string
}

// MergeResult is the provider's resulting merge commit.
type MergeResult struct {
	MergeCommitSHA string
}

// GitHub reads the repository state a tick reports.
type GitHub interface {
	State(ctx context.Context, q GitHubQuery) (GitHubState, error)
}

// GitHubQuery names what to read: the repository (owner/name), the base and
// main branches, and the label of the backlog's issues.
type GitHubQuery struct {
	Repo, Base, Main, IssueLabel string
}

// Facts is everything one tick gathered.
type Facts struct {
	Hosts     []HostSessions
	GitHub    *GitHubState
	GitHubErr error
}
