// Package orchestratortick wakes an idle orchestrator with the state of its
// workers, pull requests and CI, so a pass of its reconcile loop runs even
// when nothing else would start one.
//
// A tick reads the local daemon, the AO hosts the workers run on and GitHub,
// checks the invariants the orchestrator must keep true, and sends the
// orchestrator one message when it is idle and the state changed, or when
// nothing changed for longer than the re-nudge interval while work is in
// flight. It never interrupts an active turn. Loop repeats the tick on an
// interval until its context ends.
//
// The package has no I/O of its own: the daemon, GitHub and the state store
// are interfaces, so every decision is testable without a network.
package orchestratortick

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Config controls what a tick reads and when it sends.
type Config struct {
	// Project is the AO project whose single live orchestrator is woken.
	Project string
	// Session pins the orchestrator session id instead of looking it up.
	Session string
	// Hosts are the saved AO hosts whose worker sessions are listed.
	Hosts []string
	// Repo is the GitHub repository as owner/name.
	Repo string
	// Base is the integration branch the pull requests target.
	Base string
	// MainBranch is the branch Base must contain.
	MainBranch string
	// IssueLabel marks the backlog's task issues.
	IssueLabel string
	// BranchPrefix selects the worker sessions and branches of this backlog.
	BranchPrefix string
	// Interval is the time between ticks in Loop.
	Interval time.Duration
	// TickTimeout bounds one tick, including every daemon and GitHub call.
	TickTimeout time.Duration
	// Renudge re-sends an unchanged state while work is in flight.
	Renudge time.Duration
	// ViolationRenudge re-sends an unchanged state while an invariant is
	// violated.
	ViolationRenudge time.Duration
	// GreenPRMax is how long a green, mergeable pull request may stay open.
	GreenPRMax time.Duration
	// IdleWorkerMax is how long a worker may stay idle without a pull request.
	IdleWorkerMax time.Duration
	// AgentMixSince is when the claude-code + codex worker mix rule started.
	AgentMixSince time.Time
	// MergeAuthorized explicitly permits this cadence to merge a ready PR.
	// It defaults to false: a project rule or prompt alone is not durable
	// authorization.
	MergeAuthorized bool
	// CloseCompleted permits terminating a worker once all owned work and
	// obligations are complete. It defaults to false.
	CloseCompleted bool
	// HoldLabels prevent an otherwise ready PR from merging. Matching is
	// case-insensitive.
	HoldLabels []string
	// Paused skips every tick, like the maintenance flag in the store.
	Paused bool
}

// DefaultConfig returns the configuration of the Railway hosting backlog.
func DefaultConfig() Config {
	return Config{
		Project:          "ultra-factory",
		Hosts:            []string{"Linux", "IMac", "Z600"},
		Repo:             "petarnenov/ultra-factory",
		Base:             "feat/railway-hosting",
		MainBranch:       "main",
		IssueLabel:       "rw",
		BranchPrefix:     "rw/",
		Interval:         3 * time.Minute,
		TickTimeout:      4 * time.Minute,
		Renudge:          30 * time.Minute,
		ViolationRenudge: 10 * time.Minute,
		GreenPRMax:       10 * time.Minute,
		IdleWorkerMax:    30 * time.Minute,
		AgentMixSince:    time.Date(2026, 10, 16, 15, 45, 0, 0, time.UTC),
		HoldLabels:       []string{"needs-human", "do-not-merge", "status:blocked"},
	}
}

// ConfigFromEnv overlays the RW_TICK_* variables on DefaultConfig. Durations
// keep the units the variables always had: RW_TICK_INTERVAL in seconds, the
// *_MINUTES variables in minutes.
func ConfigFromEnv(getenv func(string) string) (Config, error) {
	cfg := DefaultConfig()
	var errs []error
	str := func(name string, dst *string) {
		if v := strings.TrimSpace(getenv(name)); v != "" {
			*dst = v
		}
	}
	dur := func(name string, unit time.Duration, dst *time.Duration) {
		v := strings.TrimSpace(getenv(name))
		if v == "" {
			return
		}
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			errs = append(errs, fmt.Errorf("%s must be a positive integer, got %q", name, v))
			return
		}
		*dst = time.Duration(n) * unit
	}
	str("RW_TICK_PROJECT", &cfg.Project)
	str("RW_TICK_SESSION", &cfg.Session)
	str("RW_TICK_REPO", &cfg.Repo)
	str("RW_TICK_BASE", &cfg.Base)
	str("RW_TICK_BRANCH_PREFIX", &cfg.BranchPrefix)
	str("RW_TICK_MAIN", &cfg.MainBranch)
	str("RW_TICK_ISSUE_LABEL", &cfg.IssueLabel)
	if v := strings.TrimSpace(getenv("RW_TICK_HOSTS")); v != "" {
		cfg.Hosts = strings.Fields(v)
	}
	dur("RW_TICK_INTERVAL", time.Second, &cfg.Interval)
	dur("RW_TICK_TIMEOUT", time.Second, &cfg.TickTimeout)
	dur("RW_TICK_RENUDGE_MINUTES", time.Minute, &cfg.Renudge)
	dur("RW_TICK_VIOLATION_RENUDGE_MINUTES", time.Minute, &cfg.ViolationRenudge)
	dur("RW_TICK_GREEN_PR_MAX_MINUTES", time.Minute, &cfg.GreenPRMax)
	dur("RW_TICK_IDLE_WORKER_MAX_MINUTES", time.Minute, &cfg.IdleWorkerMax)
	if v := strings.TrimSpace(getenv("RW_TICK_AGENT_MIX_SINCE")); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			errs = append(errs, fmt.Errorf("RW_TICK_AGENT_MIX_SINCE must be RFC 3339, got %q", v))
		} else {
			cfg.AgentMixSince = t
		}
	}
	if v := strings.TrimSpace(getenv("RW_TICK_HOLD_LABELS")); v != "" {
		cfg.HoldLabels = strings.Fields(v)
	}
	cfg.MergeAuthorized = strings.TrimSpace(getenv("RW_TICK_MERGE_AUTHORIZED")) == "1"
	cfg.CloseCompleted = strings.TrimSpace(getenv("RW_TICK_CLOSE_COMPLETED")) == "1"
	cfg.Paused = strings.TrimSpace(getenv("RW_TICK_PAUSED")) == "1"
	if err := errors.Join(errs...); err != nil {
		return Config{}, err
	}
	return cfg, cfg.Validate()
}

// Validate reports a configuration a tick cannot run with.
func (c Config) Validate() error {
	var errs []error
	if c.Project == "" && c.Session == "" {
		errs = append(errs, errors.New("a project or an orchestrator session is required"))
	}
	if owner, name, ok := strings.Cut(c.Repo, "/"); !ok || owner == "" || name == "" || strings.Contains(name, "/") {
		errs = append(errs, fmt.Errorf("repo must be owner/name, got %q", c.Repo))
	}
	if c.Base == "" || c.MainBranch == "" {
		errs = append(errs, errors.New("the base and main branches are required"))
	}
	if c.Interval <= 0 || c.TickTimeout <= 0 {
		errs = append(errs, errors.New("interval and tick timeout must be positive"))
	}
	return errors.Join(errs...)
}
