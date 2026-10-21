package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite"
)

func TestReportPullAcksAreKeptPerPuller(t *testing.T) {
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
	now := time.Now().UTC().Truncate(time.Second)
	for _, id := range []string{"rpt_a", "rpt_b"} {
		if _, err := s.CreateReport(ctx, pendingReport(id, worker, now)); err != nil {
			t.Fatal(err)
		}
	}

	if _, acked, err := s.TouchReportPuller(ctx, worker.ID, "puller-a", now, 0); err != nil || len(acked) != 0 {
		t.Fatalf("first touch: acked=%v err=%v", acked, err)
	}
	got, err := s.AcknowledgePendingSessionReportsFor(ctx, worker.ID, []string{"rpt_a", "rpt_missing"}, "puller-a", now)
	if err != nil || len(got) != 1 || got[0] != "rpt_a" {
		t.Fatalf("per-puller ack = %v, %v; want [rpt_a]", got, err)
	}
	if _, acked, _ := s.TouchReportPuller(ctx, worker.ID, "puller-a", now, 0); !acked["rpt_a"] || acked["rpt_b"] {
		t.Fatalf("puller-a acked = %v", acked)
	}
	if _, acked, _ := s.TouchReportPuller(ctx, worker.ID, "puller-b", now, 0); len(acked) != 0 {
		t.Fatalf("puller-b must not inherit puller-a's acks: %v", acked)
	}
	// A per-puller ack leaves the report pending for the worker's own queue.
	if rec, _, _ := s.GetReport(ctx, "rpt_a"); rec.DeliveryState != domain.ReportPending {
		t.Fatalf("delivery state after per-puller ack = %s, want pending", rec.DeliveryState)
	}
	// A legacy acknowledgement settles the report globally, as before.
	if got, err := s.AcknowledgePendingSessionReports(ctx, worker.ID, []string{"rpt_b"}, "pulled:legacy", now); err != nil || len(got) != 1 {
		t.Fatalf("legacy ack = %v, %v", got, err)
	}
	if rec, _, _ := s.GetReport(ctx, "rpt_b"); rec.DeliveryState != domain.ReportAcknowledged {
		t.Fatalf("legacy ack state = %s", rec.DeliveryState)
	}
}

func TestCompactExpiredReportsCountsUnacknowledgedPullers(t *testing.T) {
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
	now := time.Now().UTC().Truncate(time.Second)
	old := now.Add(-8 * 24 * time.Hour)
	for _, rec := range []domain.ReportRecord{
		pendingReport("rpt_old1", worker, old),
		pendingReport("rpt_old2", worker, old.Add(time.Second)),
		pendingReport("rpt_new", worker, now),
	} {
		if _, err := s.CreateReport(ctx, rec); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := s.TouchReportPuller(ctx, worker.ID, "puller-a", now.Add(-time.Hour), 0); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AcknowledgePendingSessionReportsFor(ctx, worker.ID, []string{"rpt_old1"}, "puller-a", now); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.TouchReportPuller(ctx, worker.ID, "puller-b", now.Add(-time.Hour), 0); err != nil {
		t.Fatal(err)
	}

	cutoff := now.Add(-7 * 24 * time.Hour)
	expired, err := s.CompactExpiredReports(ctx, worker.ID, cutoff, 10000, now)
	if err != nil || expired != 2 {
		t.Fatalf("expired = %d, %v; want 2", expired, err)
	}
	if rec, _, _ := s.GetReport(ctx, "rpt_old2"); rec.DeliveryState != domain.ReportAcknowledged {
		t.Fatalf("expired report state = %s", rec.DeliveryState)
	}
	if rec, _, _ := s.GetReport(ctx, "rpt_new"); rec.DeliveryState != domain.ReportPending {
		t.Fatalf("report inside the window expired: %s", rec.DeliveryState)
	}

	// puller-a acknowledged rpt_old1 before it expired, so it owes only rpt_old2.
	owedA, _, _ := s.TouchReportPuller(ctx, worker.ID, "puller-a", now, 0)
	owedB, _, _ := s.TouchReportPuller(ctx, worker.ID, "puller-b", now, 0)
	if owedA != 1 || owedB != 2 {
		t.Fatalf("owed a=%d b=%d; want 1 and 2", owedA, owedB)
	}
	// Reporting the count as seen subtracts it, so the next pull owes nothing.
	if owedA, _, _ = s.TouchReportPuller(ctx, worker.ID, "puller-a", now, owedA); owedA != 0 {
		t.Fatalf("owed a after seen = %d, want 0", owedA)
	}
}
