package daemon

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/config"
	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/outbox"
)

const (
	// outboxDrainInterval is how often a running daemon delivers messages the
	// CLI queued while no daemon accepted connections.
	outboxDrainInterval = 15 * time.Second
	// outboxActivityMaxAge bounds how late a queued activity signal may still
	// be applied. Reports carry no state and are delivered at any age.
	outboxActivityMaxAge = 10 * time.Minute
)

type outboxSessions interface {
	GetSession(ctx context.Context, id domain.SessionID) (domain.SessionRecord, bool, error)
}

// runOutboxDrainer delivers the CLI outbox (package outbox) now and then every
// outboxDrainInterval until ctx ends.
func runOutboxDrainer(ctx context.Context, dirs []string, handler http.Handler, sessions outboxSessions, now func() time.Time, log *slog.Logger) {
	ticker := time.NewTicker(outboxDrainInterval)
	defer ticker.Stop()
	for {
		drainOutbox(ctx, dirs, handler, sessions, now, log)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// drainOutbox delivers queued messages, oldest first, through the daemon's own
// API router, so they pass the same validation as a live request. Accepted
// (2xx) and rejected (4xx) entries are removed; others stay for the next pass.
func drainOutbox(ctx context.Context, dirs []string, handler http.Handler, sessions outboxSessions, now func() time.Time, log *slog.Logger) {
	items, rejected, errs := outbox.Pending(dirs)
	for _, err := range errs {
		log.Warn("outbox: directory skipped", "err", err)
	}
	for _, r := range rejected {
		log.Warn("outbox: invalid entry removed", "path", r.Path, "err", r.Err)
		_ = outbox.Remove(r.Path)
	}
	for _, item := range items {
		if ctx.Err() != nil {
			return
		}
		e := item.Entry
		if reason := staleOutboxActivity(ctx, e, sessions, now()); reason != "" {
			log.Info("outbox: activity signal dropped", "session", e.SessionID, "observedAt", e.ObservedAt, "reason", reason)
			_ = outbox.Remove(item.Path)
			continue
		}
		status := deliverOutboxEntry(ctx, handler, e)
		switch {
		case status >= 200 && status < 300:
			log.Info("outbox: delivered", "kind", e.Kind, "session", e.SessionID, "queuedAt", e.CreatedAt)
			_ = outbox.Remove(item.Path)
		case status >= 400 && status < 500:
			log.Warn("outbox: rejected by the API, removed", "kind", e.Kind, "session", e.SessionID, "status", status)
			_ = outbox.Remove(item.Path)
		default:
			log.Warn("outbox: delivery failed, will retry", "kind", e.Kind, "session", e.SessionID, "status", status)
		}
	}
}

// staleOutboxActivity explains why a queued activity signal must not be
// applied: it is too old, its session is gone, or newer activity has already
// been applied. A late signal must never move a session back to an older
// state.
func staleOutboxActivity(ctx context.Context, e outbox.Entry, sessions outboxSessions, now time.Time) string {
	if e.Kind != outbox.KindActivity {
		return ""
	}
	if now.Sub(e.ObservedAt) > outboxActivityMaxAge {
		return "older than " + outboxActivityMaxAge.String()
	}
	rec, ok, err := sessions.GetSession(ctx, domain.SessionID(e.SessionID))
	if err != nil {
		return ""
	}
	if !ok {
		return "session not found"
	}
	if rec.Activity.LastActivityAt.After(e.ObservedAt) {
		return "newer activity already applied"
	}
	return ""
}

// deliverOutboxEntry runs the queued request through handler as the loopback
// CLI would send it and returns the response status.
func deliverOutboxEntry(ctx context.Context, handler http.Handler, e outbox.Entry) int {
	req, err := http.NewRequestWithContext(ctx, e.Method, "/api/v1/"+e.Path, bytes.NewReader(e.Body))
	if err != nil {
		return http.StatusBadRequest
	}
	req.Host = config.LoopbackHost
	req.RemoteAddr = config.LoopbackHost + ":0"
	req.Header.Set("Content-Type", "application/json")
	rec := &statusRecorder{header: http.Header{}}
	handler.ServeHTTP(rec, req)
	if rec.status == 0 {
		return http.StatusOK
	}
	return rec.status
}

// statusRecorder keeps only the status of an in-process response.
type statusRecorder struct {
	header http.Header
	status int
}

func (r *statusRecorder) Header() http.Header { return r.header }

func (r *statusRecorder) Write(b []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	return len(b), nil
}

func (r *statusRecorder) WriteHeader(status int) {
	if r.status == 0 {
		r.status = status
	}
}
