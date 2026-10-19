package sessionhealth

import (
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

// Derive computes the session's health from durable facts at read time. It
// never mutates the record and never stores the result.
func Derive(rec domain.SessionRecord, progress domain.SessionProgress, now time.Time, cfg Config) domain.SessionHealth {
	return DeriveWithReachability(rec, progress, now, cfg, true)
}

// DeriveWithReachability adds the host-reachability distinction. An
// unreachable host is not a stalled agent.
func DeriveWithReachability(rec domain.SessionRecord, progress domain.SessionProgress, now time.Time, cfg Config, reachable bool) domain.SessionHealth {
	now = now.UTC()
	health := domain.SessionHealth{
		State:               domain.SessionHealthOK,
		StallCount:          progress.StallCount,
		LastStallForSeconds: progress.LastStallForSeconds,
	}
	if !progress.LastStallAt.IsZero() {
		at := progress.LastStallAt
		health.LastStallAt = &at
	}
	if !reachable {
		health.State = domain.SessionHealthUnreachable
		health.Reason = "host_unreachable"
		return health
	}
	if rec.IsTerminated {
		health.State = domain.SessionHealthUnknown
		health.Reason = "terminated"
		return health
	}
	if rec.ProvisionState.IsProvisioning() {
		health.State = domain.SessionHealthUnknown
		health.Reason = "provisioning"
		return health
	}
	if domain.NormalizeSessionMode(rec.Mode) == domain.SessionModeChat && rec.HibernatedAt != nil {
		health.Reason = "hibernated"
		return health
	}
	if rec.Activity.State == domain.ActivityExited {
		health.Reason = "exited"
		return health
	}

	last, class := lastProgress(progress)
	if last.IsZero() {
		// No progress yet. Fall back to the last durable activity fact so a
		// brand-new session gets a grace period.
		last = rec.Activity.LastActivityAt
		if last.IsZero() {
			last = rec.CreatedAt
		}
	}
	health.LastAt = timePointer(last)

	// A reported credential failure is its own class. Real progress after it
	// clears the fact in storage.
	blockedAt := progress.BlockedAt
	blockedCurrent := !blockedAt.IsZero() && !blockedAt.Before(last)
	if blockedAt.After(last) {
		last, class = blockedAt, ClassProvider
		health.LastAt = timePointer(last)
	}
	health.Class = string(class)
	health.InFlightTool = progress.InFlightTool

	if progress.BlockedReason != "" && blockedCurrent {
		silent := nonNegative(now.Sub(blockedAt))
		health.SilentForSeconds = int64(silent / time.Second)
		health.ThresholdSeconds = int64(cfg.BlockedAfter / time.Second)
		if silent >= cfg.BlockedAfter {
			health.State = domain.SessionHealthBlocked
			health.Reason = progress.BlockedReason
			health.Since = timePointer(blockedAt.Add(cfg.BlockedAfter))
		}
		return health
	}

	if rec.Activity.State.NeedsInput() {
		silent := nonNegative(now.Sub(last))
		health.SilentForSeconds = int64(silent / time.Second)
		health.ThresholdSeconds = int64(cfg.BlockedAfter / time.Second)
		if silent >= cfg.BlockedAfter {
			health.State = domain.SessionHealthBlocked
			health.Reason = "waiting_on_input"
			health.Since = timePointer(last.Add(cfg.BlockedAfter))
		}
		return health
	}

	if rec.Activity.State != domain.ActivityActive {
		health.Reason = "not_working"
		return health
	}

	// A tool that is still inside its own budget protects a long legitimate
	// call even when every other signal is silent.
	if progress.InFlightTool != "" {
		toolAt := progress.InFlightToolAt
		if toolAt.IsZero() {
			toolAt = progress.ToolAt
		}
		budget := cfg.ToolBudget(progress.InFlightTool)
		health.ThresholdSeconds = int64(budget / time.Second)
		if toolAt.IsZero() {
			toolAt = last
		}
		if toolAt.After(last) {
			last, class = toolAt, ClassTool
			health.LastAt = timePointer(last)
			health.Class = string(class)
		}
		if nonNegative(now.Sub(toolAt)) <= budget {
			health.Reason = "in_flight_tool"
			return health
		}
	}

	budget := cfg.BudgetFor(string(class))
	if progress.InFlightTool != "" {
		budget = cfg.ToolBudget(progress.InFlightTool)
	}
	health.ThresholdSeconds = int64(budget / time.Second)
	silent := nonNegative(now.Sub(last))
	health.SilentForSeconds = int64(silent / time.Second)
	if silent <= budget {
		return health
	}
	health.State = domain.SessionHealthStalled
	health.Reason = "no_progress"
	health.Since = timePointer(last.Add(budget))
	return health
}

// lastProgress returns the freshest timestamp and its class. Ties prefer the
// class with the strictest budget.
func lastProgress(progress domain.SessionProgress) (time.Time, SignalClass) {
	last, class := progress.ProviderAt, ClassProvider
	consider := func(at time.Time, candidate SignalClass) {
		if at.After(last) {
			last, class = at, candidate
		}
	}
	consider(progress.ToolAt, ClassTool)
	consider(progress.TerminalAt, ClassTerminal)
	consider(progress.GitAt, ClassGit)
	if last.IsZero() {
		return time.Time{}, ""
	}
	return last, class
}

func nonNegative(d time.Duration) time.Duration {
	if d < 0 {
		return 0
	}
	return d
}

func timePointer(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	value := t.UTC()
	return &value
}
