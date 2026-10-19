package sessionhealth

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

// ObserverStore is the read/write boundary for health observation.
type ObserverStore interface {
	ListAllSessions(context.Context) ([]domain.SessionRecord, error)
	ListSessionProgress(context.Context) (map[domain.SessionID]domain.SessionProgress, error)
	RecordSessionProgress(context.Context, domain.SessionID, domain.SessionProgressUpdate) error
}

// Notifier is the notification write boundary the observer uses.
type Notifier interface {
	Notify(context.Context, ports.NotificationIntent) error
	Resolve(context.Context, ports.NotificationResolution) error
}

// ObserverConfig configures the health observer.
type ObserverConfig struct {
	Health Config
	Tick   time.Duration
	Clock  func() time.Time
	Logger *slog.Logger
	// OnStalled reports one stall crossing. An error retries the next pass.
	OnStalled func(context.Context, domain.SessionRecord, domain.SessionHealth) error
	// Messenger optionally sends a checkpoint request to a stalled session.
	// Nil disables it.
	Messenger ports.AgentMessenger
	// RequestCheckpoint enables the optional session-side nudge.
	RequestCheckpoint bool
}

// Observer evaluates derived health for every session on a tick. It is not a
// detector of terminal output; it only reads durable progress facts.
type Observer struct {
	store      ObserverStore
	notifier   Notifier
	cfg        Config
	tick       time.Duration
	clock      func() time.Time
	logger     *slog.Logger
	onStalled  func(context.Context, domain.SessionRecord, domain.SessionHealth) error
	messenger  ports.AgentMessenger
	checkpoint bool
}

// NewObserver builds a health observer.
func NewObserver(store ObserverStore, notifier Notifier, cfg ObserverConfig) *Observer {
	o := &Observer{
		store: store, notifier: notifier, cfg: cfg.Health, tick: cfg.Tick,
		clock: cfg.Clock, logger: cfg.Logger, onStalled: cfg.OnStalled,
		messenger: cfg.Messenger, checkpoint: cfg.RequestCheckpoint,
	}
	if o.cfg.ProviderSilence <= 0 {
		o.cfg = DefaultConfig()
	}
	if o.tick <= 0 {
		o.tick = defaultTick
	}
	if o.clock == nil {
		o.clock = func() time.Time { return time.Now().UTC() }
	}
	if o.logger == nil {
		o.logger = slog.Default()
	}
	return o
}

// Start runs the observer until ctx ends.
func (o *Observer) Start(ctx context.Context) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(o.tick)
		defer ticker.Stop()
		for {
			if err := o.Poll(ctx); err != nil && ctx.Err() == nil {
				o.logger.Warn("session health: poll failed", "err", err)
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
	return done
}

// Poll evaluates every session once.
func (o *Observer) Poll(ctx context.Context) error {
	if o == nil || o.store == nil {
		return nil
	}
	sessions, err := o.store.ListAllSessions(ctx)
	if err != nil {
		return err
	}
	progressBySession, err := o.store.ListSessionProgress(ctx)
	if err != nil {
		return err
	}
	now := o.clock().UTC()
	for _, rec := range sessions {
		progress := progressBySession[rec.ID]
		health := Derive(rec, progress, now, o.cfg)
		if err := o.evaluate(ctx, rec, progress, health, now); err != nil {
			o.logger.Warn("session health: evaluate failed", "session", rec.ID, "err", err)
		}
	}
	return nil
}

func (o *Observer) evaluate(ctx context.Context, rec domain.SessionRecord, progress domain.SessionProgress, health domain.SessionHealth, now time.Time) error {
	active := health.State == domain.SessionHealthStalled || health.State == domain.SessionHealthBlocked
	last := timeValue(health.LastAt)
	alreadyNotified := !progress.LastNotifiedProgressAt.IsZero() && progress.LastNotifiedProgressAt.Equal(last)
	if active && !last.IsZero() && !alreadyNotified {
		if o.onStalled != nil && rec.Kind == domain.KindWorker && rec.ProjectID != "" {
			if err := o.onStalled(ctx, rec, health); err != nil {
				return err
			}
		}
		if o.notifier != nil {
			intent := ports.NotificationIntent{
				Type:               domain.NotificationSessionStalled,
				SessionID:          rec.ID,
				ProjectID:          rec.ProjectID,
				CreatedAt:          now,
				SourceKey:          fmt.Sprintf("stall:%s:%d", rec.ID, last.UnixNano()),
				SessionDisplayName: rec.DisplayName,
			}
			if err := o.notifier.Notify(ctx, intent); err != nil {
				o.logger.Warn("session health: notification failed", "session", rec.ID, "err", err)
			}
		}
		if err := o.store.RecordSessionProgress(ctx, rec.ID, domain.SessionProgressUpdate{
			LastNotifiedFor: last,
			StallCountDelta: 1,
			LastStallAt:     now,
		}); err != nil {
			return err
		}
		if o.checkpoint && health.State == domain.SessionHealthStalled && o.messenger != nil {
			message := fmt.Sprintf("AO health monitor: no progress has been observed for %s. If you are blocked, report needs_input or stuck; if you are still working, continue and ignore this message.", humanDuration(time.Duration(health.SilentForSeconds)*time.Second))
			if err := o.messenger.Send(ctx, rec.ID, message); err != nil {
				o.logger.Debug("session health: checkpoint request failed", "session", rec.ID, "err", err)
			}
		}
		return nil
	}
	if !active && !progress.LastNotifiedProgressAt.IsZero() {
		if o.notifier != nil {
			_ = o.notifier.Resolve(ctx, ports.NotificationResolution{
				Type:       domain.NotificationSessionStalled,
				SessionID:  rec.ID,
				ResolvedAt: now,
			})
		}
		update := domain.SessionProgressUpdate{ClearLastNotified: true}
		if !progress.LastStallAt.IsZero() {
			update.LastStallFor = now.Sub(progress.LastStallAt)
		}
		return o.store.RecordSessionProgress(ctx, rec.ID, update)
	}
	return nil
}

func timeValue(t *time.Time) time.Time {
	if t == nil {
		return time.Time{}
	}
	return *t
}

func humanDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int64(d/time.Second))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int64(d/time.Minute))
	}
	return fmt.Sprintf("%dh%dm", int64(d/time.Hour), int64((d%time.Hour)/time.Minute))
}
