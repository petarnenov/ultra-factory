package orchestratortick

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"time"
)

// Outcome names what one tick did.
type Outcome string

// Outcomes of a tick.
const (
	OutcomePaused         Outcome = "paused"
	OutcomeDaemonDown     Outcome = "daemon-unavailable"
	OutcomeNoOrchestrator Outcome = "no-orchestrator"
	OutcomeBusy           Outcome = "orchestrator-busy"
	OutcomeNothing        Outcome = "nothing-in-flight"
	OutcomeUnchanged      Outcome = "unchanged"
	OutcomeSent           Outcome = "sent"
	OutcomeDryRun         Outcome = "dry-run"
	OutcomeSendFailed     Outcome = "send-failed"
)

// Result is what one tick did and why.
type Result struct {
	Outcome    Outcome
	Session    string
	Reason     string
	Violations []Violation
	// Message is the text sent, or that would be sent in a dry run.
	Message string
}

// Runner runs ticks.
type Runner struct {
	Config Config
	Daemon Daemon
	GitHub GitHub
	Store  Store
	Log    *slog.Logger
	Now    func() time.Time
	// DryRun gathers and decides but never sends or records a send; the
	// message is written to DryRunOut.
	DryRun    bool
	DryRunOut io.Writer
}

func (r *Runner) now() time.Time {
	if r.Now != nil {
		return r.Now().UTC()
	}
	return time.Now().UTC()
}

func (r *Runner) log() *slog.Logger {
	if r.Log != nil {
		return r.Log
	}
	return slog.New(slog.DiscardHandler)
}

// Tick runs one tick. Its result is also logged and recorded in the store; a
// returned error means the tick could not record its state.
func (r *Runner) Tick(ctx context.Context) (Result, error) {
	res := r.tick(ctx)
	log := r.log()
	attrs := []any{"outcome", string(res.Outcome)}
	if res.Session != "" {
		attrs = append(attrs, "session", res.Session)
	}
	if res.Reason != "" {
		attrs = append(attrs, "reason", res.Reason)
	}
	for _, v := range res.Violations {
		log.Warn("invariant violated", "violation", v.Text)
	}
	switch res.Outcome {
	case OutcomeDaemonDown, OutcomeNoOrchestrator, OutcomeSendFailed:
		log.Error("tick could not wake the orchestrator", attrs...)
	default:
		log.Info("tick", attrs...)
	}
	if r.DryRun {
		if res.Message != "" && r.DryRunOut != nil {
			_, _ = fmt.Fprintln(r.DryRunOut, res.Message)
		}
		return res, nil
	}
	texts := make([]string, len(res.Violations))
	for i, v := range res.Violations {
		texts[i] = v.Text
	}
	err := r.Store.SaveTick(Record{At: r.now(), Outcome: res.Outcome, Session: res.Session, Reason: res.Reason, Violations: texts})
	return res, err
}

func (r *Runner) tick(ctx context.Context) Result {
	cfg := r.Config
	if cfg.Paused {
		return Result{Outcome: OutcomePaused, Reason: "RW_TICK_PAUSED=1"}
	}
	if reason, paused, err := r.Store.Paused(); err != nil {
		return Result{Outcome: OutcomePaused, Reason: "maintenance flag unreadable: " + err.Error()}
	} else if paused {
		return Result{Outcome: OutcomePaused, Reason: reason}
	}

	status, err := r.Daemon.Status(ctx)
	if err != nil || !status.Ready {
		detail := status.Detail
		if err != nil {
			detail = err.Error()
		}
		return Result{Outcome: OutcomeDaemonDown, Reason: "the local AO daemon is not ready (" + detail + "); nobody can be woken → start the AO daemon"}
	}
	session, err := r.orchestrator(ctx)
	if err != nil {
		return Result{Outcome: OutcomeNoOrchestrator, Reason: err.Error() + "; nobody can be woken → spawn the orchestrator, or set RW_TICK_SESSION"}
	}
	state, err := r.Daemon.Session(ctx, session)
	if err != nil {
		return Result{Outcome: OutcomeNoOrchestrator, Session: session, Reason: "orchestrator not reachable: " + err.Error()}
	}
	if state.Activity == "active" || state.Status == "working" {
		return Result{Outcome: OutcomeBusy, Session: session, Reason: fmt.Sprintf("status=%s activity=%s", state.Status, state.Activity)}
	}

	facts := r.Gather(ctx)
	now := r.now()
	violations := Violations(cfg, facts, now)
	snapshot := Snapshot(cfg, facts, violations)
	fingerprint := Fingerprint(cfg, facts, violations)
	if !InFlight(cfg, facts, violations) {
		return Result{Outcome: OutcomeNothing, Session: session}
	}

	last, sent, err := r.Store.LastSent()
	if err != nil {
		r.log().Warn("last sent state unreadable; treating the state as changed", "err", err)
		sent = false
	}
	reason := decide(cfg, last, sent, fingerprint, violations, now)
	if reason == "" {
		return Result{Outcome: OutcomeUnchanged, Session: session, Reason: "nudged recently", Violations: violations}
	}
	msg := Message(reason, snapshot, violations)
	if r.DryRun {
		return Result{Outcome: OutcomeDryRun, Session: session, Reason: reason, Violations: violations, Message: msg}
	}
	if err := r.Daemon.Send(ctx, session, msg); err != nil {
		return Result{Outcome: OutcomeSendFailed, Session: session, Reason: err.Error(), Violations: violations}
	}
	if err := r.Store.SaveSent(Sent{Session: session, Fingerprint: fingerprint, At: now}); err != nil {
		r.log().Error("sent but could not record it; the next tick will send again", "err", err)
	}
	return Result{Outcome: OutcomeSent, Session: session, Reason: reason, Violations: violations, Message: msg}
}

// decide returns why the orchestrator should be woken now, or "" when it
// should not: the state changed, or it did not change for longer than the
// re-nudge interval (shorter while an invariant is violated).
func decide(cfg Config, last Sent, sent bool, fingerprint string, violations []Violation, now time.Time) string {
	if !sent || last.Fingerprint != fingerprint {
		return "state changed"
	}
	age := now.Sub(last.At)
	if len(violations) > 0 {
		if age >= cfg.ViolationRenudge {
			return fmt.Sprintf("invariant still violated after %dm", minutes(age))
		}
		return ""
	}
	if age >= cfg.Renudge {
		return fmt.Sprintf("no change for %dm while work is in flight", minutes(age))
	}
	return ""
}

// orchestrator finds the session to wake: the pinned one, or the single live
// orchestrator of the project.
func (r *Runner) orchestrator(ctx context.Context) (string, error) {
	cfg := r.Config
	if cfg.Session != "" {
		state, err := r.Daemon.Session(ctx, cfg.Session)
		if err != nil {
			return "", fmt.Errorf("orchestrator %s (RW_TICK_SESSION) does not exist: %w", cfg.Session, err)
		}
		if state.Terminated {
			return "", fmt.Errorf("orchestrator %s (RW_TICK_SESSION) is terminated", cfg.Session)
		}
		return cfg.Session, nil
	}
	all, err := r.Daemon.Orchestrators(ctx)
	if err != nil {
		return "", fmt.Errorf("listing orchestrators failed: %w", err)
	}
	var live []string
	for _, o := range all {
		if o.ProjectID == cfg.Project {
			live = append(live, o.ID)
		}
	}
	switch len(live) {
	case 1:
		return live[0], nil
	case 0:
		return "", fmt.Errorf("no live orchestrator session in project %s", cfg.Project)
	default:
		return "", fmt.Errorf("%d live orchestrators in project %s (%s); set RW_TICK_SESSION to the one to wake", len(live), cfg.Project, strings.Join(live, ", "))
	}
}

// Gather reads every host and GitHub concurrently.
func (r *Runner) Gather(ctx context.Context) Facts {
	facts := Facts{Hosts: make([]HostSessions, len(r.Config.Hosts))}
	var wg sync.WaitGroup
	for i, host := range r.Config.Hosts {
		wg.Add(1)
		go func() {
			defer wg.Done()
			workers, err := r.Daemon.Workers(ctx, host)
			facts.Hosts[i] = HostSessions{Host: host, Sessions: workers, Err: err}
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		state, err := r.GitHub.State(ctx, GitHubQuery{Repo: r.Config.Repo, Base: r.Config.Base, Main: r.Config.MainBranch, IssueLabel: r.Config.IssueLabel})
		if err != nil {
			facts.GitHubErr = err
			return
		}
		facts.GitHub = &state
	}()
	wg.Wait()
	return facts
}

// Loop runs a tick now and then every Config.Interval until ctx ends. Each
// tick is bounded by Config.TickTimeout; a failing or panicking tick is
// logged and never ends the loop.
func (r *Runner) Loop(ctx context.Context) error {
	log := r.log()
	log.Info("tick loop started", "interval", r.Config.Interval.String(), "project", r.Config.Project, "hosts", strings.Join(r.Config.Hosts, " "))
	timer := time.NewTimer(0)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			log.Info("tick loop stopped")
			return nil
		case <-timer.C:
		}
		r.safeTick(ctx)
		timer.Reset(r.Config.Interval)
	}
}

func (r *Runner) safeTick(ctx context.Context) {
	defer func() {
		if p := recover(); p != nil {
			r.log().Error("tick panicked", "panic", fmt.Sprint(p))
		}
	}()
	tickCtx, cancel := context.WithTimeout(ctx, r.Config.TickTimeout)
	defer cancel()
	if _, err := r.Tick(tickCtx); err != nil {
		r.log().Error("tick state not recorded", "err", err)
	}
}
