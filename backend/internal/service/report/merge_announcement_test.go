package report

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

func (s *coordinatorStore) CreateReport(_ context.Context, rec domain.ReportRecord) (domain.ReportRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.createErr != nil {
		return domain.ReportRecord{}, s.createErr
	}
	s.reports = append(s.reports, rec)
	return rec, nil
}

type prOutcomeFixture struct {
	reports  *Service
	delivery *Coordinator
	store    *coordinatorStore
	sent     *coordinatorDelivery
	now      time.Time
}

// newPROutcomeFixture seeds a root, a coordinator, and a worker owned by
// owner (nil means root-owned).
func newPROutcomeFixture(t *testing.T, owner *domain.SessionID) prOutcomeFixture {
	t.Helper()
	now := time.Date(2026, 10, 23, 9, 0, 0, 0, time.UTC)
	store := newCoordinatorStore(now)
	store.sessions = []domain.SessionRecord{
		{ID: "root", ProjectID: "p", Kind: domain.KindOrchestrator},
		{ID: "coordinator", ProjectID: "p", Kind: domain.KindCoordinator},
		{ID: "worker", ProjectID: "p", Kind: domain.KindWorker, OwnerSessionID: owner},
	}
	sent := &coordinatorDelivery{}
	clock := func() time.Time { return now }
	ids := 0
	reports := New(Deps{Store: store, Claims: store, Now: clock, NewID: func() string {
		ids++
		return fmt.Sprintf("rpt_%d", ids)
	}})
	delivery := NewCoordinator(CoordinatorDeps{Store: store, Delivery: sent, Now: clock})
	return prOutcomeFixture{reports: reports, delivery: delivery, store: store, sent: sent, now: now}
}

func (f prOutcomeFixture) deliverDue(t *testing.T) {
	t.Helper()
	if _, err := f.delivery.RunDue(context.Background()); err != nil {
		t.Fatalf("run due: %v", err)
	}
}

func (f prOutcomeFixture) submissionsTo(target domain.SessionID, needle string) int {
	n := 0
	for _, s := range f.sent.submissions {
		if s.target == target && strings.Contains(s.message, needle) {
			n++
		}
	}
	return n
}

func TestAnnounceMergedReachesTheRootForARootOwnedWorker(t *testing.T) {
	f := newPROutcomeFixture(t, nil)
	ctx := context.Background()

	for range 2 {
		if err := f.reports.AnnounceMerged(ctx, "worker", "https://example/pr/201", 201, "merge-commit"); err != nil {
			t.Fatal(err)
		}
	}
	f.deliverDue(t)

	if got := f.submissionsTo("root", "PR #201 merged (merge commit merge-commit)"); got != 1 {
		t.Fatalf("root notices = %d, want 1; sent = %+v", got, f.sent.submissions)
	}
}

func TestAnnounceMergedReachesTheOwningCoordinator(t *testing.T) {
	owner := domain.SessionID("coordinator")
	f := newPROutcomeFixture(t, &owner)

	if err := f.reports.AnnounceMerged(context.Background(), "worker", "https://example/pr/201", 201, "merge-commit"); err != nil {
		t.Fatal(err)
	}
	f.deliverDue(t)

	if f.submissionsTo("coordinator", "PR #201 merged") != 1 || f.submissionsTo("root", "PR #201") != 0 {
		t.Fatalf("sent = %+v, want one notice to the coordinator only", f.sent.submissions)
	}
}

func TestAnnounceMergedIsDueImmediately(t *testing.T) {
	f := newPROutcomeFixture(t, nil)

	if err := f.reports.AnnounceMerged(context.Background(), "worker", "https://example/pr/201", 201, "merge-commit"); err != nil {
		t.Fatal(err)
	}

	if len(f.store.reports) != 1 || !f.store.reports[0].AvailableAt.Equal(f.now) {
		t.Fatalf("reports = %+v, want one due at %v, not after the batch window", f.store.reports, f.now)
	}
}

func TestAnnounceMergedStillReportsATerminatedWorker(t *testing.T) {
	f := newPROutcomeFixture(t, nil)
	f.store.sessions[2].IsTerminated = true

	if err := f.reports.AnnounceMerged(context.Background(), "worker", "https://example/pr/201", 201, "merge-commit"); err != nil {
		t.Fatal(err)
	}
	f.deliverDue(t)

	if f.submissionsTo("root", "PR #201 merged") != 1 {
		t.Fatalf("merge after termination not reported; sent = %+v", f.sent.submissions)
	}
}

func TestAnnounceMergedWithoutAMergeCommitIsNotDropped(t *testing.T) {
	f := newPROutcomeFixture(t, nil)
	ctx := context.Background()

	// A fast-forward or null mergeCommit reports no SHA; the PR identity dedupes.
	for range 2 {
		if err := f.reports.AnnounceMerged(ctx, "worker", "https://example/pr/203", 203, ""); err != nil {
			t.Fatal(err)
		}
	}
	f.deliverDue(t)

	if f.submissionsTo("root", "PR #203 merged (no merge commit reported)") != 1 {
		t.Fatalf("sent = %+v, want one notice", f.sent.submissions)
	}
}

func TestAnnounceClosedReportsOnceAndApartFromMerge(t *testing.T) {
	f := newPROutcomeFixture(t, nil)
	ctx := context.Background()
	closedAt := f.now.Add(-time.Minute)

	for range 2 {
		if err := f.reports.AnnounceClosed(ctx, "worker", "https://example/pr/204", 204, closedAt); err != nil {
			t.Fatal(err)
		}
	}
	if err := f.reports.AnnounceMerged(ctx, "worker", "https://example/pr/204", 204, ""); err != nil {
		t.Fatal(err)
	}
	f.deliverDue(t)

	if f.submissionsTo("root", "PR #204 closed without merge") != 1 || f.submissionsTo("root", "PR #204 merged") != 1 {
		t.Fatalf("sent = %+v, want one closed and one merged notice", f.sent.submissions)
	}
}

func TestAnnounceMergedReleasesTheClaimWhenTheReportIsNotStored(t *testing.T) {
	f := newPROutcomeFixture(t, nil)
	ctx := context.Background()
	f.store.createErr = errors.New("disk full")

	if err := f.reports.AnnounceMerged(ctx, "worker", "https://example/pr/201", 201, "merge-commit"); err == nil {
		t.Fatal("store failure was not returned")
	}
	f.store.createErr = nil
	if err := f.reports.AnnounceMerged(ctx, "worker", "https://example/pr/201", 201, "merge-commit"); err != nil {
		t.Fatal(err)
	}

	if len(f.store.reports) != 1 {
		t.Fatalf("reports = %+v, want the retry to store one", f.store.reports)
	}
}

func TestAnnounceMergedIgnoresNonWorkersAndStandaloneSessions(t *testing.T) {
	f := newPROutcomeFixture(t, nil)
	ctx := context.Background()
	f.store.sessions = append(f.store.sessions, domain.SessionRecord{ID: "standalone", Kind: domain.KindWorker})

	for _, id := range []domain.SessionID{"coordinator", "standalone", "missing"} {
		if err := f.reports.AnnounceMerged(ctx, id, "https://example/pr/9", 9, "sha-"+string(id)); err != nil {
			t.Fatal(err)
		}
	}

	if len(f.store.reports) != 0 {
		t.Fatalf("reports = %+v, want none", f.store.reports)
	}
}

func TestAnnounceClosedReportsAReopenedPRThatClosesAgain(t *testing.T) {
	f := newPROutcomeFixture(t, nil)
	ctx := context.Background()

	for _, closedAt := range []time.Time{f.now.Add(-time.Hour), f.now} {
		if err := f.reports.AnnounceClosed(ctx, "worker", "https://example/pr/205", 205, closedAt); err != nil {
			t.Fatal(err)
		}
	}

	if len(f.store.reports) != 2 {
		t.Fatalf("reports = %+v, want one per close", f.store.reports)
	}
}
