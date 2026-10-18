package stagehistory

import (
	"context"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	sessionsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/session"
)

type fakeSessions struct{ list []domain.Session }

func (f *fakeSessions) List(context.Context, sessionsvc.ListFilter) ([]domain.Session, error) {
	return f.list, nil
}

type appended struct {
	id    domain.SessionID
	stage domain.KanbanColumn
	at    time.Time
}

type fakeStore struct {
	latest map[domain.SessionID]domain.KanbanColumn
	rows   []appended
}

func (f *fakeStore) ListLatestSessionStages(context.Context) (map[domain.SessionID]domain.KanbanColumn, error) {
	out := make(map[domain.SessionID]domain.KanbanColumn, len(f.latest))
	for k, v := range f.latest {
		out[k] = v
	}
	return out, nil
}

func (f *fakeStore) AppendSessionStage(_ context.Context, id domain.SessionID, stage domain.KanbanColumn, at time.Time) error {
	f.rows = append(f.rows, appended{id, stage, at})
	f.latest[id] = stage
	return nil
}

func session(id string, kind domain.SessionKind, column domain.KanbanColumn, terminated bool, readiness string) domain.Session {
	return domain.Session{
		SessionRecord: domain.SessionRecord{ID: domain.SessionID(id), Kind: kind, IsTerminated: terminated},
		KanbanColumn:  column, StatusReadiness: readiness,
	}
}

func TestPollAppendsOnlyObservedChanges(t *testing.T) {
	now := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	sessions := &fakeSessions{list: []domain.Session{
		session("ao-1", domain.KindWorker, domain.KanbanBuilding, false, "ready"),
		session("ao-2", domain.KindWorker, domain.KanbanArchive, true, "ready"),
		session("ao-3", domain.KindOrchestrator, domain.KanbanBuilding, false, "ready"),
		session("ao-4", domain.KindWorker, domain.KanbanValidating, false, "checking"),
		session("ao-5", domain.KindWorker, domain.KanbanArchive, true, "ready"),
	}}
	store := &fakeStore{latest: map[domain.SessionID]domain.KanbanColumn{"ao-5": domain.KanbanReady}}
	obs := New(Deps{Sessions: sessions, Store: store, Now: func() time.Time { return now }})

	if err := obs.Poll(context.Background()); err != nil {
		t.Fatal(err)
	}
	want := []appended{{"ao-1", domain.KanbanBuilding, now}, {"ao-5", domain.KanbanArchive, now}}
	if len(store.rows) != len(want) {
		t.Fatalf("rows = %+v, want %+v", store.rows, want)
	}
	for i := range want {
		if store.rows[i] != want[i] {
			t.Fatalf("rows = %+v, want %+v", store.rows, want)
		}
	}

	if err := obs.Poll(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(store.rows) != 2 {
		t.Fatalf("unchanged columns appended again: %+v", store.rows)
	}

	sessions.list[0].KanbanColumn = domain.KanbanValidating
	if err := obs.Poll(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(store.rows) != 3 || store.rows[2].stage != domain.KanbanValidating {
		t.Fatalf("transition not appended: %+v", store.rows)
	}
}
