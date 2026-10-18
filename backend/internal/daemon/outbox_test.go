package daemon

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/outbox"
)

type fakeOutboxSessions map[domain.SessionID]domain.SessionRecord

func (f fakeOutboxSessions) GetSession(_ context.Context, id domain.SessionID) (domain.SessionRecord, bool, error) {
	rec, ok := f[id]
	return rec, ok, nil
}

type apiCall struct {
	method, path, host, remote, body string
}

func outboxHandler(status int, calls *[]apiCall) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		*calls = append(*calls, apiCall{r.Method, r.URL.Path, r.Host, r.RemoteAddr, string(body)})
		w.WriteHeader(status)
	})
}

func queue(t *testing.T, dir string, e outbox.Entry) string {
	t.Helper()
	path, err := outbox.Write([]string{dir}, e)
	if err != nil {
		t.Fatal(err)
	}
	return path
}

func pendingCount(t *testing.T, dir string) int {
	t.Helper()
	items, _, _ := outbox.Pending([]string{dir})
	return len(items)
}

func TestDrainOutboxDeliversReportsThroughTheRouter(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "outbox")
	now := time.Unix(10_000, 0).UTC()
	queue(t, dir, outbox.Entry{Version: outbox.Version, Kind: outbox.KindReport, Method: "POST", Path: "reports",
		Body: json.RawMessage(`{"sessionId":"ao-1","message":"done"}`), SessionID: "ao-1", CreatedAt: now.Add(-time.Hour)})

	for _, tc := range []struct {
		status    int
		remaining int
	}{
		{http.StatusServiceUnavailable, 1}, // kept for the next pass
		{http.StatusCreated, 0},
	} {
		var calls []apiCall
		drainOutbox(context.Background(), []string{dir}, outboxHandler(tc.status, &calls), fakeOutboxSessions{}, func() time.Time { return now }, slog.New(slog.DiscardHandler))
		if len(calls) != 1 {
			t.Fatalf("status %d: %d API calls, want 1", tc.status, len(calls))
		}
		c := calls[0]
		if c.method != http.MethodPost || c.path != "/api/v1/reports" || c.host != "127.0.0.1" || c.body != `{"sessionId":"ao-1","message":"done"}` {
			t.Fatalf("status %d: call = %+v", tc.status, c)
		}
		if got := pendingCount(t, dir); got != tc.remaining {
			t.Fatalf("status %d: %d entries left, want %d", tc.status, got, tc.remaining)
		}
	}
}

func TestDrainOutboxRemovesRejectedEntries(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "outbox")
	now := time.Unix(10_000, 0).UTC()
	queue(t, dir, outbox.Entry{Version: outbox.Version, Kind: outbox.KindReport, Method: "POST", Path: "reports",
		Body: json.RawMessage(`{}`), SessionID: "ao-1", CreatedAt: now})
	var calls []apiCall
	drainOutbox(context.Background(), []string{dir}, outboxHandler(http.StatusUnprocessableEntity, &calls), fakeOutboxSessions{}, func() time.Time { return now }, slog.New(slog.DiscardHandler))
	if got := pendingCount(t, dir); got != 0 {
		t.Fatalf("%d entries left after a 4xx, want 0", got)
	}
}

// A late activity signal must never move a session back to an older state.
func TestDrainOutboxDropsStaleActivity(t *testing.T) {
	now := time.Unix(10_000, 0).UTC()
	observed := now.Add(-time.Minute)
	activity := func(sessionID string, at time.Time) outbox.Entry {
		return outbox.Entry{Version: outbox.Version, Kind: outbox.KindActivity, Method: "POST", Path: "sessions/" + sessionID + "/activity",
			Body: json.RawMessage(`{"state":"idle"}`), SessionID: sessionID, CreatedAt: at, ObservedAt: at}
	}
	sessions := fakeOutboxSessions{
		"fresh": {Activity: domain.Activity{LastActivityAt: observed.Add(-time.Second)}},
		"newer": {Activity: domain.Activity{LastActivityAt: observed.Add(time.Second)}},
		"old":   {Activity: domain.Activity{}},
	}
	for name, tc := range map[string]struct {
		entry     outbox.Entry
		delivered bool
	}{
		"fresh signal":               {activity("fresh", observed), true},
		"newer activity was applied": {activity("newer", observed), false},
		"older than the maximum age": {activity("old", now.Add(-outboxActivityMaxAge-time.Second)), false},
		"session no longer exists":   {activity("gone", observed), false},
	} {
		t.Run(name, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "outbox")
			queue(t, dir, tc.entry)
			var calls []apiCall
			drainOutbox(context.Background(), []string{dir}, outboxHandler(http.StatusNoContent, &calls), sessions, func() time.Time { return now }, slog.New(slog.DiscardHandler))
			if (len(calls) == 1) != tc.delivered {
				t.Fatalf("delivered = %v, want %v", len(calls) == 1, tc.delivered)
			}
			if got := pendingCount(t, dir); got != 0 {
				t.Fatalf("%d entries left, want 0", got)
			}
		})
	}
}
