package daemon

import (
	"context"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite"
)

func TestDaemonReportServiceFilesEachPROutcomeOnce(t *testing.T) {
	ctx := context.Background()
	store, err := sqlite.Open(t.TempDir())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	now := time.Now().UTC()
	if err := store.UpsertProject(ctx, domain.ProjectRecord{ID: "ao", Path: "/tmp/ao-report-wiring", RegisteredAt: now}); err != nil {
		t.Fatalf("seed project: %v", err)
	}
	worker, err := store.CreateSession(ctx, domain.SessionRecord{ProjectID: "ao", Kind: domain.KindWorker, Harness: domain.HarnessClaudeCode,
		Activity: domain.Activity{State: domain.ActivityActive, LastActivityAt: now}, CreatedAt: now, UpdatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	created := 0
	reports := newReportService(store, nil, func(domain.ReportRecord) { created++ })

	for range 2 {
		if err := reports.AnnounceMerged(ctx, worker.ID, "https://example/pr/1", 1, "sha-1"); err != nil {
			t.Fatalf("announce merged: %v", err)
		}
		if err := reports.AnnounceClosed(ctx, worker.ID, "https://example/pr/2", 2, now); err != nil {
			t.Fatalf("announce closed: %v", err)
		}
	}

	if created != 2 {
		t.Fatalf("reports created = %d, want one per PR outcome", created)
	}
}
