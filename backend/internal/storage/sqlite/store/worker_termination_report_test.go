package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite"
)

func terminationReports(t *testing.T, s *sqlite.Store, project domain.ProjectID, id domain.SessionID) []domain.ReportRecord {
	t.Helper()
	all, err := s.ListReportsByProject(context.Background(), project)
	if err != nil {
		t.Fatal(err)
	}
	var out []domain.ReportRecord
	for _, r := range all {
		if r.SessionID == id && r.Note == "AO: the worker session ended." {
			out = append(out, r)
		}
	}
	return out
}

func setTerminated(t *testing.T, s *sqlite.Store, rec *domain.SessionRecord, terminated bool, at time.Time) {
	t.Helper()
	rec.IsTerminated = terminated
	rec.UpdatedAt = at
	if err := s.UpdateSession(context.Background(), *rec); err != nil {
		t.Fatal(err)
	}
}

func TestWorkerTerminationFilesOneDueReportPerTermination(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	seedProject(t, s, "ao")
	now := time.Now().UTC().Truncate(time.Second)
	worker, err := s.CreateSession(ctx, domain.SessionRecord{ProjectID: "ao", Kind: domain.KindWorker, Harness: domain.HarnessClaudeCode,
		Activity: domain.Activity{State: domain.ActivityActive, LastActivityAt: now}, CreatedAt: now, UpdatedAt: now})
	if err != nil {
		t.Fatal(err)
	}

	setTerminated(t, s, &worker, true, now)
	setTerminated(t, s, &worker, true, now.Add(time.Second))
	got := terminationReports(t, s, "ao", worker.ID)
	if len(got) != 1 || got[0].State != domain.ReportCheckpoint || !got[0].AvailableAt.Equal(now) {
		t.Fatalf("reports = %+v, want one checkpoint due at %v", got, now)
	}
	pending, err := s.ListPendingReports(ctx, now, 10)
	if err != nil || len(pending) != 1 {
		t.Fatalf("pending at termination = %+v err=%v, want the report due", pending, err)
	}

	// A restored worker that ends again is reported again.
	setTerminated(t, s, &worker, false, now.Add(2*time.Second))
	setTerminated(t, s, &worker, true, now.Add(3*time.Second))
	if got := terminationReports(t, s, "ao", worker.ID); len(got) != 2 {
		t.Fatalf("reports after restore and second end = %d, want 2", len(got))
	}
}

func TestOnlyWorkerTerminationFilesAReport(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	seedProject(t, s, "ao")
	now := time.Now().UTC().Truncate(time.Second)
	for kind, scope := range map[domain.SessionKind]string{domain.KindOrchestrator: "", domain.KindCoordinator: "scope"} {
		rec, err := s.CreateSession(ctx, domain.SessionRecord{ProjectID: "ao", Kind: kind, CoordinatorScope: scope, Harness: domain.HarnessClaudeCode,
			Activity: domain.Activity{State: domain.ActivityActive, LastActivityAt: now}, CreatedAt: now, UpdatedAt: now})
		if err != nil {
			t.Fatalf("create %s: %v", kind, err)
		}
		setTerminated(t, s, &rec, true, now)
		if got := terminationReports(t, s, "ao", rec.ID); len(got) != 0 {
			t.Fatalf("%s termination filed %+v", kind, got)
		}
	}
}
