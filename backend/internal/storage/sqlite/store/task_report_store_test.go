package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite"
)

func TestSessionConfigHistoryRecordsInsertAndModelChanges(t *testing.T) {
	ctx := context.Background()
	s, err := sqlite.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	seedProject(t, s, "ao")
	rec := sampleRecord("ao")
	rec.Metadata.Model = "claude-opus-5-5"
	rec.Metadata.Effort = "medium"
	sess, err := s.CreateSession(ctx, rec)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpdateSessionModel(ctx, sess.ID, "claude-opus-5-5"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpdateSessionModel(ctx, sess.ID, "claude-sonnet-5-5"); err != nil {
		t.Fatal(err)
	}
	got, err := s.ListSessionConfigHistory(ctx, sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("history = %+v, want insert plus one real change", got)
	}
	if got[0].Source != domain.ConfigChangeSession || got[0].Model != "claude-opus-5-5" || got[0].Effort != "medium" ||
		got[0].Harness != string(domain.HarnessClaudeCode) {
		t.Fatalf("first row = %+v", got[0])
	}
	if got[1].Model != "claude-sonnet-5-5" || got[1].Effort != "medium" || got[1].RecordedAt.IsZero() {
		t.Fatalf("second row = %+v", got[1])
	}
}

func TestSessionStageTransitionsAppendAndLatest(t *testing.T) {
	ctx := context.Background()
	s, err := sqlite.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	seedProject(t, s, "ao")
	a, err := s.CreateSession(ctx, sampleRecord("ao"))
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.CreateSession(ctx, sampleRecord("ao"))
	if err != nil {
		t.Fatal(err)
	}
	t0 := time.Date(2026, 10, 18, 10, 0, 0, 0, time.UTC)
	for _, step := range []struct {
		id    domain.SessionID
		stage domain.KanbanColumn
		at    time.Time
	}{
		{a.ID, domain.KanbanBuilding, t0},
		{a.ID, domain.KanbanValidating, t0.Add(time.Minute)},
		{b.ID, domain.KanbanBuilding, t0.Add(2 * time.Minute)},
		{a.ID, domain.KanbanBuilding, t0.Add(3 * time.Minute)},
	} {
		if err := s.AppendSessionStage(ctx, step.id, step.stage, step.at); err != nil {
			t.Fatal(err)
		}
	}
	latest, err := s.ListLatestSessionStages(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if latest[a.ID] != domain.KanbanBuilding || latest[b.ID] != domain.KanbanBuilding || len(latest) != 2 {
		t.Fatalf("latest = %v", latest)
	}
	got, err := s.ListSessionStageTransitions(ctx, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || got[1].Stage != domain.KanbanValidating || !got[1].EnteredAt.Equal(t0.Add(time.Minute)) {
		t.Fatalf("transitions = %+v", got)
	}
	if err := s.AppendSessionStage(ctx, a.ID, "bogus", t0); err == nil {
		t.Fatal("unknown stage was accepted")
	}
	runs, err := s.ListSessionUsageRuns(ctx, a.ID)
	if err != nil || len(runs) != 0 {
		t.Fatalf("usage runs = %v, %v; want none recorded", runs, err)
	}
}
