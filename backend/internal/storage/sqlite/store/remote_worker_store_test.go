package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite"
)

func pendingReport(id string, sess domain.SessionRecord, at time.Time) domain.ReportRecord {
	return domain.ReportRecord{
		ID: id, SessionID: sess.ID, ProjectID: sess.ProjectID,
		State: domain.ReportNeedsInput, Note: "need a decision",
		CreatedAt: at, DeliveryState: domain.ReportPending, AvailableAt: at, RepeatCount: 1,
	}
}

func TestAcknowledgePendingSessionReportsTouchesOnlyThatWorkersPendingReports(t *testing.T) {
	ctx := context.Background()
	s, err := sqlite.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	seedProject(t, s, "ao")
	worker, err := s.CreateSession(ctx, sampleRecord("ao"))
	if err != nil {
		t.Fatal(err)
	}
	other, err := s.CreateSession(ctx, sampleRecord("ao"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	// rpt_claimed is held by this daemon's own coordinator delivery.
	if _, err := s.CreateReport(ctx, pendingReport("rpt_claimed", worker, now)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ClaimPendingReportBatch(ctx, "ao", "root", "root", "own-delivery", now); err != nil {
		t.Fatal(err)
	}
	for _, rec := range []domain.ReportRecord{
		pendingReport("rpt_mine", worker, now.Add(time.Second)),
		pendingReport("rpt_other", other, now.Add(time.Second)),
	} {
		if _, err := s.CreateReport(ctx, rec); err != nil {
			t.Fatal(err)
		}
	}

	got, err := s.AcknowledgePendingSessionReports(ctx, worker.ID, []string{"rpt_mine", "rpt_claimed", "rpt_other", "rpt_missing"}, "pulled:x", now)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != "rpt_mine" {
		t.Fatalf("acknowledged = %v, want [rpt_mine]", got)
	}
	mine, _, _ := s.GetReport(ctx, "rpt_mine")
	if mine.DeliveryState != domain.ReportAcknowledged || mine.ClaimToken != "pulled:x" || mine.AcknowledgedAt.IsZero() {
		t.Fatalf("rpt_mine = %+v", mine)
	}
	if rec, _, _ := s.GetReport(ctx, "rpt_claimed"); rec.DeliveryState != domain.ReportClaimed || rec.ClaimToken != "own-delivery" {
		t.Fatalf("this daemon's claimed report changed: %+v", rec)
	}
	if rec, _, _ := s.GetReport(ctx, "rpt_other"); rec.DeliveryState != domain.ReportPending {
		t.Fatalf("another worker's report changed: %+v", rec)
	}
}

func TestRemoteReportHistoryIsHostScopedAndKeepsOutputs(t *testing.T) {
	ctx := context.Background()
	s, err := sqlite.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	seedProject(t, s, "ao")
	now := time.Now().UTC().Truncate(time.Second)
	for _, host := range []string{"host-a", "host-b"} {
		link := domain.RemoteWorkerLink{HostID: host, HostLabel: host, RemoteSessionID: "same-session", RemoteProjectID: "remote-project", ProjectID: "ao", CreatedAt: now}
		if err := s.UpsertRemoteWorkerLink(ctx, link); err != nil {
			t.Fatal(err)
		}
		report := domain.ReportRecord{ID: "same-report", SessionID: link.RemoteSessionID, ProjectID: "ao", HostID: host,
			HostLabel: host, Agent: "codex", SessionDisplayName: host + " task", State: domain.ReportDone, Note: "completed",
			Outputs:   []domain.ReportOutput{{Kind: domain.ReportOutputArtifact, Reference: host + "/result.md", Label: "Result"}},
			CreatedAt: now, RepeatCount: 1}
		if err := s.RecordRemoteReportHistory(ctx, host, link.RemoteSessionID, []domain.ReportRecord{report}); err != nil {
			t.Fatal(err)
		}
	}
	reports, err := s.ListReportsByProject(ctx, "ao")
	if err != nil {
		t.Fatal(err)
	}
	if len(reports) != 2 || reports[0].HostID != "host-a" || reports[1].HostID != "host-b" {
		t.Fatalf("remote history = %+v", reports)
	}
	for _, report := range reports {
		if report.ID != "same-report" || report.SessionID != "same-session" || report.Agent != "codex" || len(report.Outputs) != 1 || report.Outputs[0].Label != "Result" {
			t.Fatalf("remote report projection = %+v", report)
		}
	}
}

func TestRemoteWorkerLinkLifecycleAndDeliveryHistory(t *testing.T) {
	ctx := context.Background()
	s, err := sqlite.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	seedProject(t, s, "ao")
	created := time.Now().UTC().Truncate(time.Second)
	link := domain.RemoteWorkerLink{
		HostID: "host-build", HostLabel: "build-box", RemoteSessionID: "widgets-3", RemoteProjectID: "widgets",
		ProjectID: "ao", DisplayName: "auth refactor", CreatedAt: created,
	}
	if err := s.UpsertRemoteWorkerLink(ctx, domain.RemoteWorkerLink{HostID: "host-build"}); err == nil {
		t.Fatal("an incomplete link was stored")
	}
	if err := s.UpsertRemoteWorkerLink(ctx, link); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordDeliveredRemoteReports(ctx, link.HostID, link.RemoteSessionID, []string{"rpt_1", "rpt_2"}, created); err != nil {
		t.Fatal(err)
	}
	// Recording again is a no-op, and a refreshed link keeps its history and creation time.
	if err := s.RecordDeliveredRemoteReports(ctx, link.HostID, link.RemoteSessionID, []string{"rpt_2"}, created); err != nil {
		t.Fatal(err)
	}
	refreshed := link
	refreshed.DisplayName = "renamed"
	refreshed.CreatedAt = created.Add(time.Hour)
	if err := s.UpsertRemoteWorkerLink(ctx, refreshed); err != nil {
		t.Fatal(err)
	}
	links, err := s.ListRemoteWorkerLinks(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(links) != 1 || links[0].DisplayName != "renamed" || !links[0].CreatedAt.Equal(created) || links[0].HostLabel != "build-box" || links[0].RemoteProjectID != "widgets" {
		t.Fatalf("links = %+v", links)
	}
	delivered, err := s.ListDeliveredRemoteReports(ctx, link.HostID, link.RemoteSessionID)
	if err != nil || len(delivered) != 2 || !delivered["rpt_1"] || !delivered["rpt_2"] {
		t.Fatalf("delivered = %v err=%v", delivered, err)
	}

	if err := s.DeleteRemoteWorkerLink(ctx, link.HostID, link.RemoteSessionID); err != nil {
		t.Fatal(err)
	}
	if links, _ := s.ListRemoteWorkerLinks(ctx); len(links) != 0 {
		t.Fatalf("link survived deletion: %+v", links)
	}
	if delivered, _ := s.ListDeliveredRemoteReports(ctx, link.HostID, link.RemoteSessionID); len(delivered) != 0 {
		t.Fatalf("delivery history survived its link: %v", delivered)
	}
	// Recording against a deleted link is refused by the foreign key.
	if err := s.RecordDeliveredRemoteReports(ctx, link.HostID, link.RemoteSessionID, []string{"rpt_3"}, created); err == nil {
		t.Fatal("delivery recorded for a link that no longer exists")
	}
}
