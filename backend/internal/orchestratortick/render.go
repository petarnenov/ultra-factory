package orchestratortick

import (
	"fmt"
	"strings"
	"time"
)

// Snapshot renders the state a tick reports, with one "VIOLATION:" line per
// violated invariant.
func Snapshot(cfg Config, facts Facts, violations []Violation, now time.Time) string {
	return snapshotWithCompletion(cfg, facts, violations, nil, now)
}

// snapshotWithCompletion renders the state plus the durable completion result,
// so a remaining obligation is visible in the tick message and tick status.
func snapshotWithCompletion(cfg Config, facts Facts, violations []Violation, completions []CompletionNote, now time.Time) string {
	out := render(cfg, facts, violations, now, func(v Violation) string { return v.Text })
	if len(completions) == 0 {
		return out
	}
	var b strings.Builder
	b.WriteString(out)
	b.WriteString("[completion]\n")
	for _, line := range CompletionLines(completions) {
		fmt.Fprintf(&b, "  %s\n", line)
	}
	return b.String()
}

// Fingerprint renders the state with each violation's stable key instead of its
// text, so two ticks of the same state match even as durations grow.
func Fingerprint(cfg Config, facts Facts, violations []Violation, now time.Time) string {
	return render(cfg, facts, violations, now, func(v Violation) string { return v.Key })
}

func render(cfg Config, facts Facts, violations []Violation, now time.Time, show func(Violation) string) string {
	var b strings.Builder
	for _, h := range facts.Hosts {
		fmt.Fprintf(&b, "[%s]\n", h.Host)
		if h.Err != nil {
			fmt.Fprintf(&b, "  unreachable: %v\n", h.Err)
			continue
		}
		for _, w := range h.Sessions {
			if !strings.HasPrefix(w.Branch, cfg.BranchPrefix) {
				continue
			}
			pr, ci := "-", w.CI
			if w.PRNumber != 0 {
				pr = fmt.Sprintf("#%d", w.PRNumber)
			}
			if ci == "" {
				ci = "-"
			}
			fmt.Fprintf(&b, "  %s %s pr=%s ci=%s %s\n", w.ID, w.Branch, pr, ci, w.Activity)
		}
	}
	fmt.Fprintf(&b, "[open PRs -> %s]\n", cfg.Base)
	if facts.GitHub != nil {
		for _, pr := range facts.GitHub.OpenPRs {
			mergeable := pr.Mergeable
			if pr.Draft {
				mergeable = "NO(draft)"
			}
			fmt.Fprintf(&b, "  #%d %s mergeable=%s checks=%s\n", pr.Number, pr.Head, mergeable, PRCheckSummary(pr, now))
		}
	}
	b.WriteString("[invariants]\n")
	for _, v := range violations {
		fmt.Fprintf(&b, "VIOLATION: %s\n", show(v))
	}
	return b.String()
}

// InFlight reports whether there is anything for the orchestrator to act on:
// a backlog worker, an open pull request, or a violated invariant.
func InFlight(cfg Config, facts Facts, violations []Violation) bool {
	if len(violations) > 0 {
		return true
	}
	if facts.GitHub != nil && len(facts.GitHub.OpenPRs) > 0 {
		return true
	}
	for _, h := range facts.Hosts {
		for _, w := range h.Sessions {
			if strings.HasPrefix(w.Branch, cfg.BranchPrefix) {
				return true
			}
		}
	}
	return false
}

// Message is the text sent to the orchestrator.
func Message(reason, snapshot string, violations []Violation) string {
	var b strings.Builder
	fmt.Fprintf(&b, "TICK (automatic wake-up from ao orchestrator tick, not a message from the user; reason: %s).\n", reason)
	if len(violations) > 0 {
		b.WriteString("\nFIRST fix these violations, before anything else in the loop:\n")
		for _, v := range violations {
			fmt.Fprintf(&b, "VIOLATION: %s\n", v.Text)
		}
		b.WriteString("\n")
	}
	b.WriteString("\nHere is the current state of the workers, pull requests and invariants:\n")
	b.WriteString(snapshot)
	b.WriteString("Run a full pass per \"Wake-up and reconcile loop\" in your instructions: merge every green PR that meets the criteria, nudge stalled workers, start every task whose dependencies are merged, update #59.")
	return b.String()
}
