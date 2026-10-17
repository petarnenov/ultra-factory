package orchestratortick

import (
	"context"
	"time"
)

// Worker is one AO session on a host, as a tick needs it.
type Worker struct {
	ID         string
	Branch     string
	Activity   string
	Harness    string
	Terminated bool
	// PRNumber and CI describe the session's first pull request; PRNumber 0
	// means it has none.
	PRNumber       int
	CI             string
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

// PullRequest is an open pull request to the base branch.
type PullRequest struct {
	Number    int
	Head      string
	Mergeable string
	Checks    []Check
}

// MergedPR is a pull request already merged into the base branch.
type MergedPR struct {
	Number int
	Body   string
}

// GitHubState is what a tick reads from GitHub.
type GitHubState struct {
	OpenPRs   []PullRequest
	MergedPRs []MergedPR
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
