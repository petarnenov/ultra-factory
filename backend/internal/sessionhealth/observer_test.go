package sessionhealth

import (
	"context"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

type fakeStore struct {
	sessions []domain.SessionRecord
	progress map[domain.SessionID]domain.SessionProgress
	updates  []domain.SessionProgressUpdate
}

func (f *fakeStore) ListAllSessions(context.Context) ([]domain.SessionRecord, error) {
	return f.sessions, nil
}

func (f *fakeStore) ListSessionProgress(context.Context) (map[domain.SessionID]domain.SessionProgress, error) {
	out := make(map[domain.SessionID]domain.SessionProgress, len(f.progress))
	for id, progress := range f.progress {
		out[id] = progress
	}
	return out, nil
}

func (f *fakeStore) RecordSessionProgress(_ context.Context, id domain.SessionID, update domain.SessionProgressUpdate) error {
	f.updates = append(f.updates, update)
	progress := f.progress[id]
	if !update.ProviderAt.IsZero() && update.ProviderAt.After(progress.ProviderAt) {
		progress.ProviderAt = update.ProviderAt
	}
	if !update.ToolAt.IsZero() && update.ToolAt.After(progress.ToolAt) {
		progress.ToolAt = update.ToolAt
	}
	if !update.TerminalAt.IsZero() && update.TerminalAt.After(progress.TerminalAt) {
		progress.TerminalAt = update.TerminalAt
	}
	if !update.GitAt.IsZero() && update.GitAt.After(progress.GitAt) {
		progress.GitAt = update.GitAt
	}
	if update.InFlightTool != nil {
		progress.InFlightTool = *update.InFlightTool
	}
	if update.ClearInFlight {
		progress.InFlightTool = ""
	}
	if update.BlockedReason != nil {
		progress.BlockedReason = *update.BlockedReason
		progress.BlockedAt = update.BlockedAt
	}
	if update.ClearBlocked {
		progress.BlockedAt = time.Time{}
		progress.BlockedReason = ""
	}
	if update.ClearLastNotified {
		progress.LastNotifiedProgressAt = time.Time{}
	} else if update.LastNotifiedFor.After(progress.LastNotifiedProgressAt) {
		progress.LastNotifiedProgressAt = update.LastNotifiedFor
	}
	progress.StallCount += update.StallCountDelta
	if update.LastStallAt.After(progress.LastStallAt) {
		progress.LastStallAt = update.LastStallAt
	}
	if update.LastStallFor > 0 {
		progress.LastStallForSeconds = int64(update.LastStallFor / time.Second)
	}
	f.progress[id] = progress
	return nil
}

type fakeNotifier struct {
	intents     []ports.NotificationIntent
	resolutions []ports.NotificationResolution
}

func (f *fakeNotifier) Notify(_ context.Context, intent ports.NotificationIntent) error {
	f.intents = append(f.intents, intent)
	return nil
}

func (f *fakeNotifier) Resolve(_ context.Context, resolution ports.NotificationResolution) error {
	f.resolutions = append(f.resolutions, resolution)
	return nil
}

func TestObserverReportsOneStallEpisodeAndResolvesOnRecovery(t *testing.T) {
	now := time.Unix(10_000, 0).UTC()
	store := &fakeStore{
		sessions: []domain.SessionRecord{{
			ID: "s-1", ProjectID: "p-1", Kind: domain.KindWorker,
			Activity: domain.Activity{State: domain.ActivityActive},
		}},
		progress: map[domain.SessionID]domain.SessionProgress{
			"s-1": {ProviderAt: now.Add(-30 * time.Minute)},
		},
	}
	notifier := &fakeNotifier{}
	reports, recoveries := 0, 0
	observer := NewObserver(store, notifier, ObserverConfig{
		Health: DefaultConfig(),
		Clock:  func() time.Time { return now },
		OnStalled: func(context.Context, domain.SessionRecord, domain.SessionHealth) error {
			reports++
			return nil
		},
		OnRecovered: func(context.Context, domain.SessionRecord, domain.SessionProgress, domain.SessionHealth) error {
			recoveries++
			return nil
		},
	})

	if err := observer.Poll(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(notifier.intents) != 1 || notifier.intents[0].Type != domain.NotificationSessionStalled {
		t.Fatalf("intents = %+v, want one session_stalled", notifier.intents)
	}
	if reports != 1 {
		t.Fatalf("reports = %d, want 1", reports)
	}
	if store.progress["s-1"].StallCount != 1 {
		t.Fatalf("stall count = %d, want 1", store.progress["s-1"].StallCount)
	}

	// The same silent episode must not report again.
	if err := observer.Poll(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(notifier.intents) != 1 || reports != 1 {
		t.Fatalf("duplicate report: intents=%d reports=%d", len(notifier.intents), reports)
	}

	// Real progress resolves the open notification and records the episode
	// length for tuning false positives.
	recovered := withProgress(store.progress["s-1"], now.Add(-time.Second))
	recovered.LastStallAt = now.Add(-5 * time.Minute)
	store.progress["s-1"] = recovered
	if err := observer.Poll(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(notifier.resolutions) != 1 || notifier.resolutions[0].Type != domain.NotificationSessionStalled {
		t.Fatalf("resolutions = %+v, want one session_stalled", notifier.resolutions)
	}
	if store.progress["s-1"].LastStallForSeconds == 0 {
		t.Fatalf("last stall duration was not recorded: %+v", store.progress["s-1"])
	}
	if recoveries != 1 {
		t.Fatalf("recoveries = %d, want 1", recoveries)
	}
}

func TestObserverDoesNotReportRecoveryForTerminatedSession(t *testing.T) {
	now := time.Unix(10_000, 0).UTC()
	rec := domain.SessionRecord{
		ID: "s-1", ProjectID: "p-1", Kind: domain.KindWorker,
		Activity: domain.Activity{State: domain.ActivityActive},
	}
	store := &fakeStore{
		sessions: []domain.SessionRecord{rec},
		progress: map[domain.SessionID]domain.SessionProgress{
			"s-1": {ProviderAt: now.Add(-30 * time.Minute)},
		},
	}
	notifier := &fakeNotifier{}
	recoveries := 0
	observer := NewObserver(store, notifier, ObserverConfig{
		Health: DefaultConfig(),
		Clock:  func() time.Time { return now },
		OnRecovered: func(context.Context, domain.SessionRecord, domain.SessionProgress, domain.SessionHealth) error {
			recoveries++
			return nil
		},
	})
	if err := observer.Poll(context.Background()); err != nil {
		t.Fatal(err)
	}
	rec.IsTerminated = true
	store.sessions[0] = rec
	if err := observer.Poll(context.Background()); err != nil {
		t.Fatal(err)
	}
	if recoveries != 0 {
		t.Fatalf("terminated session reported recovery %d times", recoveries)
	}
	if len(notifier.resolutions) != 1 {
		t.Fatalf("resolutions = %+v, want the stall notification resolved", notifier.resolutions)
	}
}

func withProgress(progress domain.SessionProgress, at time.Time) domain.SessionProgress {
	progress.ProviderAt = at
	return progress
}

func TestRecorderWritesToolBoundaryWithName(t *testing.T) {
	store := &fakeStore{progress: map[domain.SessionID]domain.SessionProgress{}}
	recorder := NewRecorder(store)
	now := time.Unix(10_000, 0).UTC()
	if err := recorder.Record(context.Background(), "s-1", Signal{Class: ClassTool, At: now, ToolName: "Bash"}); err != nil {
		t.Fatal(err)
	}
	if got := store.progress["s-1"].InFlightTool; got != "Bash" {
		t.Fatalf("in-flight tool = %q, want Bash", got)
	}
	if err := recorder.Record(context.Background(), "s-1", Signal{Class: ClassTool, At: now.Add(time.Second), ToolEnd: true}); err != nil {
		t.Fatal(err)
	}
	if got := store.progress["s-1"].InFlightTool; got != "" {
		t.Fatalf("in-flight tool = %q, want cleared", got)
	}
}
