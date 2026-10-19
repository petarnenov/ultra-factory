package sessionhealth

import (
	"context"
	"strings"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

// SignalClass names the boundary that produced a progress heartbeat.
type SignalClass string

// Progress signal classes.
const (
	ClassProvider SignalClass = "provider"
	ClassTool     SignalClass = "tool"
	ClassTerminal SignalClass = "terminal"
	ClassGit      SignalClass = "git"
)

// Signal is one progress heartbeat. A tool start sets the in-flight tool and
// a tool end clears it; BlockedReason records a durable blocked observation.
type Signal struct {
	Class         SignalClass
	At            time.Time
	ToolName      string
	ToolEnd       bool
	BlockedReason string
}

// ProgressStore is the narrow persistence boundary for progress facts.
type ProgressStore interface {
	RecordSessionProgress(context.Context, domain.SessionID, domain.SessionProgressUpdate) error
}

// Recorder turns a boundary event into one durable progress update.
type Recorder struct {
	store ProgressStore
	now   func() time.Time
}

// NewRecorder builds a recorder over the store.
func NewRecorder(store ProgressStore) *Recorder {
	return &Recorder{store: store, now: func() time.Time { return time.Now().UTC() }}
}

// Record writes one signal. A zero At uses the recorder clock.
func (r *Recorder) Record(ctx context.Context, id domain.SessionID, signal Signal) error {
	if r == nil || r.store == nil || id == "" || signal.Class == "" {
		return nil
	}
	at := signal.At.UTC()
	if at.IsZero() {
		at = r.now()
	}
	update := domain.SessionProgressUpdate{}
	switch signal.Class {
	case ClassProvider:
		update.ProviderAt = at
	case ClassTool:
		update.ToolAt = at
		name := strings.TrimSpace(signal.ToolName)
		if name == "" {
			name = "tool"
		}
		if signal.ToolEnd {
			update.ClearInFlight = true
		} else {
			update.InFlightTool = &name
			update.InFlightToolAt = at
		}
	case ClassTerminal:
		update.TerminalAt = at
	case ClassGit:
		update.GitAt = at
	}
	if reason := strings.TrimSpace(signal.BlockedReason); reason != "" {
		update.BlockedAt = at
		update.BlockedReason = &reason
	}
	return r.store.RecordSessionProgress(ctx, id, update)
}

// RecordBlocked records a blocked observation without advancing progress.
func (r *Recorder) RecordBlocked(ctx context.Context, id domain.SessionID, reason string, at time.Time) error {
	if r == nil || r.store == nil || id == "" || strings.TrimSpace(reason) == "" {
		return nil
	}
	if at.IsZero() {
		at = r.now()
	}
	reason = strings.TrimSpace(reason)
	return r.store.RecordSessionProgress(ctx, id, domain.SessionProgressUpdate{
		BlockedAt:     at.UTC(),
		BlockedReason: &reason,
	})
}

// ClearBlocked removes a blocked fact after real progress arrived.
func (r *Recorder) ClearBlocked(ctx context.Context, id domain.SessionID) error {
	if r == nil || r.store == nil || id == "" {
		return nil
	}
	return r.store.RecordSessionProgress(ctx, id, domain.SessionProgressUpdate{ClearBlocked: true})
}
