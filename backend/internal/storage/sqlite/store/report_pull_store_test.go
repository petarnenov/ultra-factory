package store_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	reportsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/report"
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

	if _, acked, err := s.TouchReportPuller(ctx, worker.ID, "puller-a", now); err != nil || len(acked) != 0 {
		t.Fatalf("first touch: acked=%v err=%v", acked, err)
	}
	got, err := s.AcknowledgePendingSessionReportsFor(ctx, worker.ID, []string{"rpt_a", "rpt_missing"}, "puller-a", now)
	if err != nil || len(got) != 1 || got[0] != "rpt_a" {
		t.Fatalf("per-puller ack = %v, %v; want [rpt_a]", got, err)
	}
	if _, acked, _ := s.TouchReportPuller(ctx, worker.ID, "puller-a", now); !acked["rpt_a"] || acked["rpt_b"] {
		t.Fatalf("puller-a acked = %v", acked)
	}
	if _, acked, _ := s.TouchReportPuller(ctx, worker.ID, "puller-b", now); len(acked) != 0 {
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
	if _, _, err := s.TouchReportPuller(ctx, worker.ID, "puller-a", now.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AcknowledgePendingSessionReportsFor(ctx, worker.ID, []string{"rpt_old1"}, "puller-a", now); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.TouchReportPuller(ctx, worker.ID, "puller-b", now.Add(-time.Hour)); err != nil {
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

	// puller-a acked rpt_old1 before it expired, so it misses only rpt_old2.
	totalA, _, _ := s.TouchReportPuller(ctx, worker.ID, "puller-a", now)
	totalB, _, _ := s.TouchReportPuller(ctx, worker.ID, "puller-b", now)
	if totalA != 1 || totalB != 2 {
		t.Fatalf("totals a=%d b=%d; want 1 and 2", totalA, totalB)
	}
	// The total is monotonic: a repeated touch returns the same value.
	if again, _, _ := s.TouchReportPuller(ctx, worker.ID, "puller-b", now); again != 2 {
		t.Fatalf("repeated touch total = %d, want 2", again)
	}
}

// A pull repeated with the same expiredSeen returns the same unseen count, so
// a lost response is harmless: the puller simply sends its last total again.
func TestPullRepeatedWithSameExpiredSeenIsIdempotent(t *testing.T) {
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
	for _, rec := range []domain.ReportRecord{
		pendingReport("rpt_old1", worker, now),
		pendingReport("rpt_old2", worker, now.Add(time.Second)),
	} {
		if _, err := s.CreateReport(ctx, rec); err != nil {
			t.Fatal(err)
		}
	}
	// The clock moves past retention, so both reports expire on the next pull.
	clock := now
	svc := reportsvc.New(reportsvc.Deps{Store: s, Now: func() time.Time { return clock }})
	if _, err := svc.PullFor(ctx, worker.ID, reportsvc.PullOptions{PullerID: "puller-a"}); err != nil {
		t.Fatal(err)
	}
	clock = now.Add(8 * 24 * time.Hour)
	pull := func(seen int64) reportsvc.PullResult {
		t.Helper()
		result, err := svc.PullFor(ctx, worker.ID, reportsvc.PullOptions{PullerID: "puller-a", ExpiredSeen: seen})
		if err != nil {
			t.Fatal(err)
		}
		return result
	}

	first := pull(0)
	if first.ExpiredBeforePull != 2 || first.ExpiredTotal != 2 {
		t.Fatalf("first pull = unseen %d total %d, want 2 and 2", first.ExpiredBeforePull, first.ExpiredTotal)
	}
	// The first response is lost, so the same expiredSeen is sent again.
	if retry := pull(0); retry.ExpiredBeforePull != 2 {
		t.Fatalf("retry with the same expiredSeen = %d, want 2", retry.ExpiredBeforePull)
	}
	// The puller logged total 2 and sends it back; a repeat owes nothing.
	if seen := pull(2); seen.ExpiredBeforePull != 0 {
		t.Fatalf("pull after logging = %d, want 0", seen.ExpiredBeforePull)
	}
	if repeat := pull(2); repeat.ExpiredBeforePull != 0 || repeat.ExpiredTotal != 2 {
		t.Fatalf("repeat after logging = unseen %d total %d, want 0 and 2", repeat.ExpiredBeforePull, repeat.ExpiredTotal)
	}
}

// Peer-acked reports leave the local schedule, so a remote worker's backlog
// cannot fill the scan window and starve a local worker's report.
func TestPeerAckedReportsDoNotStarveTheLocalSchedule(t *testing.T) {
	ctx := context.Background()
	s, err := sqlite.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	seedProject(t, s, "ao")
	remote, err := s.CreateSession(ctx, sampleRecord("ao"))
	if err != nil {
		t.Fatal(err)
	}
	local, err := s.CreateSession(ctx, sampleRecord("ao"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	const backlog = 300
	ids := make([]string, backlog)
	for i := range backlog {
		ids[i] = fmt.Sprintf("rpt_remote_%03d", i)
		if _, err := s.CreateReport(ctx, pendingReport(ids[i], remote, now.Add(time.Duration(i)*time.Millisecond))); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.CreateReport(ctx, pendingReport("rpt_local", local, now.Add(time.Hour))); err != nil {
		t.Fatal(err)
	}
	if acked, err := s.AcknowledgePendingSessionReportsFor(ctx, remote.ID, ids, "daemon-a", now); err != nil || len(acked) != backlog {
		t.Fatalf("per-puller ack = %d, %v", len(acked), err)
	}

	due, err := s.ListPendingReportSchedule(ctx, 256)
	if err != nil {
		t.Fatal(err)
	}
	if len(due) != 1 || due[0].ID != "rpt_local" {
		t.Fatalf("schedule = %v, want only rpt_local", due)
	}
}
