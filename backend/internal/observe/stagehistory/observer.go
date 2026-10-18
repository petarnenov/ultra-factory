// Package stagehistory logs when workers enter derived Kanban columns, as
// report history only; current status is still derived on every read.
package stagehistory

import (
	"context"
	"log/slog"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/observe"
	sessionsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/session"
)

// DefaultInterval bounds how late a recorded transition can be.
const DefaultInterval = 15 * time.Second

// Sessions is the session read model whose derived columns are observed.
type Sessions interface {
	List(ctx context.Context, filter sessionsvc.ListFilter) ([]domain.Session, error)
}

// Store persists the observed transitions.
type Store interface {
	ListLatestSessionStages(ctx context.Context) (map[domain.SessionID]domain.KanbanColumn, error)
	AppendSessionStage(ctx context.Context, id domain.SessionID, stage domain.KanbanColumn, at time.Time) error
}

// Deps configures an Observer.
type Deps struct {
	Sessions Sessions
	Store    Store
	Interval time.Duration
	Now      func() time.Time
	Logger   *slog.Logger
}

// Observer polls derived columns and appends changes.
type Observer struct {
	sessions Sessions
	store    Store
	interval time.Duration
	now      func() time.Time
	logger   *slog.Logger
}

// New constructs an Observer with defaults for unset dependencies.
func New(d Deps) *Observer {
	if d.Interval <= 0 {
		d.Interval = DefaultInterval
	}
	if d.Now == nil {
		d.Now = time.Now
	}
	if d.Logger == nil {
		d.Logger = slog.Default()
	}
	return &Observer{sessions: d.Sessions, store: d.Store, interval: d.Interval, now: d.Now, logger: d.Logger}
}

// Start runs Poll on every interval until ctx is done.
func (o *Observer) Start(ctx context.Context) <-chan struct{} {
	return observe.StartPollLoop(ctx, o.interval, o.Poll, o.logger, "stage history observer")
}

// Poll appends a row when a worker's column differs from its latest one. A
// worker first seen terminated gets none: its archive time is unknown.
func (o *Observer) Poll(ctx context.Context) error {
	sessions, err := o.sessions.List(ctx, sessionsvc.ListFilter{})
	if err != nil {
		return err
	}
	latest, err := o.store.ListLatestSessionStages(ctx)
	if err != nil {
		return err
	}
	now := o.now().UTC()
	for _, sess := range sessions {
		if sess.Kind != domain.KindWorker || sess.KanbanColumn == "" {
			continue
		}
		if sess.StatusReadiness != "" && sess.StatusReadiness != "ready" {
			continue
		}
		prev, seen := latest[sess.ID]
		if prev == sess.KanbanColumn || (!seen && sess.IsTerminated) {
			continue
		}
		if err := o.store.AppendSessionStage(ctx, sess.ID, sess.KanbanColumn, now); err != nil {
			return err
		}
	}
	return nil
}
