package report

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

func TestCoordinatorFixedFallbackAndSettlementBatch(t *testing.T) {
	now := time.Date(2026, 9, 14, 10, 0, 0, 0, time.UTC)
	store := newCoordinatorStore(now)
	store.sessions = []domain.SessionRecord{{ID: "orch", ProjectID: "p", Kind: domain.KindOrchestrator, Mode: domain.SessionModeChat}}
	store.add(reportAt("one", "worker", now, now.Add(time.Hour), "first"))
	store.add(reportAt("two", "worker", now.Add(20*time.Minute), now.Add(80*time.Minute), "second"))
	delivery := &coordinatorDelivery{}
	c := NewCoordinator(CoordinatorDeps{Store: store, Delivery: delivery, Now: func() time.Time { return now }, NewToken: func() string { return "claim" }})

	if delay, err := c.RunDue(context.Background()); err != nil || delay != time.Hour {
		t.Fatalf("before fallback delay = %v, err = %v", delay, err)
	}
	now = now.Add(time.Hour)
	if _, err := c.RunDue(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(delivery.submissions) != 1 || !strings.Contains(delivery.submissions[0].message, "first") || !strings.Contains(delivery.submissions[0].message, "second") {
		t.Fatalf("submission = %#v, want one shared batch", delivery.submissions)
	}
	if store.acked != 2 {
		t.Fatalf("acknowledged = %d, want 2", store.acked)
	}

	store.add(reportAt("done-1", "worker", now, now.Add(5*time.Minute), "done"))
	store.add(reportAt("done-2", "worker", now.Add(2*time.Minute), now.Add(7*time.Minute), "later done"))
	now = now.Add(5 * time.Minute)
	if _, err := c.RunDue(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(delivery.submissions) != 2 || !strings.Contains(delivery.submissions[1].message, "later done") {
		t.Fatalf("settlement submission = %#v", delivery.submissions)
	}
}

func TestCoordinatorRoutesAtAttemptAndLeavesPendingWithoutOrchestrator(t *testing.T) {
	now := time.Date(2026, 9, 14, 11, 0, 0, 0, time.UTC)
	store := newCoordinatorStore(now)
	store.add(reportAt("need", "worker", now, now, "decision"))
	delivery := &coordinatorDelivery{}
	c := NewCoordinator(CoordinatorDeps{Store: store, Delivery: delivery, Now: func() time.Time { return now }, RetryDelay: time.Second})
	if delay, err := c.RunDue(context.Background()); err == nil || delay != time.Second {
		t.Fatalf("no orchestrator delay = %v, err = %v", delay, err)
	}
	if store.reports[0].DeliveryState != domain.ReportPending {
		t.Fatalf("state = %s, want pending", store.reports[0].DeliveryState)
	}
	store.sessions = []domain.SessionRecord{
		{ID: "old", ProjectID: "p", Kind: domain.KindOrchestrator, Mode: domain.SessionModeChat, IsTerminated: true, CreatedAt: now},
		{ID: "replacement", ProjectID: "p", Kind: domain.KindOrchestrator, Mode: domain.SessionModeChat, CreatedAt: now.Add(time.Second)},
	}
	if _, err := c.RunDue(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(delivery.submissions) != 1 || delivery.submissions[0].target != "replacement" {
		t.Fatalf("submissions = %#v", delivery.submissions)
	}
}

func TestCoordinatorRoutesEachOwnerGroupSeparately(t *testing.T) {
	now := time.Date(2026, 9, 14, 11, 30, 0, 0, time.UTC)
	store := newCoordinatorStore(now)
	owner := domain.SessionID("coord-a")
	store.sessions = []domain.SessionRecord{
		{ID: "root", ProjectID: "p", Kind: domain.KindOrchestrator, Mode: domain.SessionModeChat, CreatedAt: now},
		{ID: "coord-a", ProjectID: "p", Kind: domain.KindCoordinator, CoordinatorScope: "auth", Mode: domain.SessionModeChat, CreatedAt: now},
		{ID: "worker-a", ProjectID: "p", Kind: domain.KindWorker, OwnerSessionID: &owner},
		{ID: "worker-root", ProjectID: "p", Kind: domain.KindWorker},
	}
	store.add(
		reportAt("owned", "worker-a", now, now, "owned work"),
		reportAt("rooted", "worker-root", now, now, "root work"),
	)
	delivery := &coordinatorDelivery{}
	c := NewCoordinator(CoordinatorDeps{Store: store, Delivery: delivery, Now: func() time.Time { return now }})

	if _, err := c.RunDue(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(delivery.submissions) != 2 {
		t.Fatalf("submissions = %#v, want one turn per owner group", delivery.submissions)
	}
	got := map[domain.SessionID]string{}
	for _, sub := range delivery.submissions {
		got[sub.target] = sub.message
	}
	if !strings.Contains(got["coord-a"], "owned work") || strings.Contains(got["coord-a"], "root work") {
		t.Fatalf("coordinator batch = %q", got["coord-a"])
	}
	if !strings.Contains(got["root"], "root work") || strings.Contains(got["root"], "owned work") {
		t.Fatalf("root batch = %q", got["root"])
	}
	if store.acked != 2 {
		t.Fatalf("acknowledged = %d, want 2", store.acked)
	}
}

func TestCoordinatorRepairsTerminatedOwnerToRoot(t *testing.T) {
	now := time.Date(2026, 9, 14, 11, 45, 0, 0, time.UTC)
	store := newCoordinatorStore(now)
	owner := domain.SessionID("coord-gone")
	store.sessions = []domain.SessionRecord{
		{ID: "root", ProjectID: "p", Kind: domain.KindOrchestrator, Mode: domain.SessionModeChat, CreatedAt: now},
		{ID: "coord-gone", ProjectID: "p", Kind: domain.KindCoordinator, CoordinatorScope: "auth", IsTerminated: true, CreatedAt: now},
		{ID: "worker", ProjectID: "p", Kind: domain.KindWorker, OwnerSessionID: &owner},
	}
	store.add(reportAt("report", "worker", now, now, "orphaned report"))
	delivery := &coordinatorDelivery{}
	c := NewCoordinator(CoordinatorDeps{Store: store, Delivery: delivery, Now: func() time.Time { return now }})

	if _, err := c.RunDue(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(delivery.submissions) != 1 || delivery.submissions[0].target != "root" {
		t.Fatalf("submissions = %#v, want the root fallback", delivery.submissions)
	}
	if len(store.ownerEvents) != 1 || store.ownerEvents[0].reason != domain.OwnershipOwnerNotLiveReconciled || store.ownerEvents[0].owner != nil {
		t.Fatalf("owner repair = %#v", store.ownerEvents)
	}
}

func TestCoordinatorDeliversToOwnerWhenRootIsAbsent(t *testing.T) {
	now := time.Date(2026, 9, 14, 11, 50, 0, 0, time.UTC)
	store := newCoordinatorStore(now)
	owner := domain.SessionID("coord-a")
	store.sessions = []domain.SessionRecord{
		{ID: "coord-a", ProjectID: "p", Kind: domain.KindCoordinator, CoordinatorScope: "auth", Mode: domain.SessionModeChat, CreatedAt: now},
		{ID: "worker-a", ProjectID: "p", Kind: domain.KindWorker, OwnerSessionID: &owner},
	}
	store.add(reportAt("owned", "worker-a", now, now, "owned work"))
	delivery := &coordinatorDelivery{}
	c := NewCoordinator(CoordinatorDeps{Store: store, Delivery: delivery, Now: func() time.Time { return now }})

	if _, err := c.RunDue(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(delivery.submissions) != 1 || delivery.submissions[0].target != "coord-a" {
		t.Fatalf("submissions = %#v, want coordinator delivery without a root", delivery.submissions)
	}
}

func TestCoordinatorStuckInterruptRateLimitAndCoalescing(t *testing.T) {
	now := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
	store := newCoordinatorStore(now)
	store.sessions = []domain.SessionRecord{{ID: "orch", ProjectID: "p", Kind: domain.KindOrchestrator, Mode: domain.SessionModeChat}}
	first := reportAt("stuck-1", "worker", now, now, "cannot continue")
	first.State = domain.ReportStuck
	second := reportAt("stuck-2", "worker", now.Add(time.Second), now, "cannot continue")
	second.State = domain.ReportStuck
	store.add(first, second)
	delivery := &coordinatorDelivery{}
	c := NewCoordinator(CoordinatorDeps{Store: store, Delivery: delivery, Now: func() time.Time { return now }})
	if _, err := c.RunDue(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(delivery.interrupts) != 1 {
		t.Fatalf("interrupts = %v", delivery.interrupts)
	}
	if !strings.Contains(delivery.submissions[0].message, "repeated 2 times") {
		t.Fatalf("message = %q", delivery.submissions[0].message)
	}
	repeat := reportAt("stuck-3", "worker", now.Add(time.Minute), now, "cannot continue")
	repeat.State = domain.ReportStuck
	store.add(repeat)
	if _, err := c.RunDue(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(delivery.interrupts) != 1 {
		t.Fatalf("rate limited interrupts = %v", delivery.interrupts)
	}
	if len(delivery.submissions) != 1 {
		t.Fatalf("throttled repeat created an extra wakeup: %#v", delivery.submissions)
	}
}

func TestPreparePiggybackClaimsOnlyTheCoordinatorsGroup(t *testing.T) {
	now := time.Date(2026, 9, 14, 13, 50, 0, 0, time.UTC)
	store := newCoordinatorStore(now)
	owner := domain.SessionID("coord-a")
	store.sessions = []domain.SessionRecord{
		{ID: "root", ProjectID: "p", Kind: domain.KindOrchestrator, Mode: domain.SessionModeChat, CreatedAt: now},
		{ID: "coord-a", ProjectID: "p", Kind: domain.KindCoordinator, CoordinatorScope: "auth", Mode: domain.SessionModeChat, CreatedAt: now},
		{ID: "worker-a", ProjectID: "p", Kind: domain.KindWorker, OwnerSessionID: &owner},
		{ID: "worker-root", ProjectID: "p", Kind: domain.KindWorker},
	}
	store.add(
		reportAt("owned", "worker-a", now, now, "owned work"),
		reportAt("rooted", "worker-root", now, now, "root work"),
	)
	c := NewCoordinator(CoordinatorDeps{Store: store, Delivery: &coordinatorDelivery{}, Now: func() time.Time { return now }, NewToken: func() string { return "piggyback" }})
	batch, err := c.PreparePiggyback(context.Background(), "coord-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(batch.Reports) != 1 || batch.Reports[0].ID != "owned" {
		t.Fatalf("piggyback batch = %+v, want only the coordinator's worker", batch.Reports)
	}
}

func TestReportSessionLinkUsesDisplayName(t *testing.T) {
	got := reportSessionLink(domain.ReportRecord{
		SessionID: "worker/1", ProjectID: "proj 1", SessionDisplayName: "Backend [worker]",
	})
	want := `[Backend \[worker\]](ao://sessions/proj%201/worker%2F1)`
	if got != want {
		t.Fatalf("link = %q, want %q", got, want)
	}
}

func TestPreparedBatchRenderIncludesExplicitTerminalOutcome(t *testing.T) {
	report := reportAt("final", "worker", time.Now(), time.Now(), "task ended")
	report.State = domain.ReportDone
	report.TerminalOutcome = domain.ReportOutcomeFailure
	got := (PreparedBatch{Reports: []domain.ReportRecord{report}}).Render()
	if !strings.Contains(got, "[done/failure]") {
		t.Fatalf("rendered final report omitted its terminal outcome: %q", got)
	}
}

func TestCoordinatorAcceptanceAndPiggybackFencing(t *testing.T) {
	now := time.Date(2026, 9, 14, 13, 0, 0, 0, time.UTC)
	store := newCoordinatorStore(now)
	store.sessions = []domain.SessionRecord{{ID: "orch", ProjectID: "p", Kind: domain.KindOrchestrator, Mode: domain.SessionModeChat}}
	store.add(reportAt("info", "worker", now, now.Add(time.Hour), "context"))
	c := NewCoordinator(CoordinatorDeps{Store: store, Delivery: &coordinatorDelivery{}, Now: func() time.Time { return now }, NewToken: func() string { return "piggyback-token" }})
	batch, err := c.PreparePiggyback(context.Background(), "orch")
	if err != nil || len(batch.Reports) != 1 {
		t.Fatalf("prepare = %#v, err = %v", batch, err)
	}
	combined := batch.AppendToUserMessage("hello")
	if !strings.HasPrefix(combined, "hello\n\n<ao-worker-reports>\n") ||
		!strings.HasSuffix(combined, "\n</ao-worker-reports>") {
		t.Fatalf("piggyback text = %q", combined)
	}
	if !strings.Contains(combined, "[checkpoint] [worker](ao://sessions/p/worker)") {
		t.Fatalf("piggyback link = %q", combined)
	}
	if store.acked != 0 {
		t.Fatal("claimed report acknowledged before acceptance")
	}
	if err := c.AcceptPiggyback(context.Background(), batch); err != nil {
		t.Fatal(err)
	}
	if store.acked != 1 {
		t.Fatalf("acknowledged = %d", store.acked)
	}
	if count, _ := store.AcknowledgeReportBatch(context.Background(), "p", "stale-token", now); count != 0 {
		t.Fatalf("stale token acknowledged %d reports", count)
	}
}

func TestResolveRemoteRecipientUsesTheLinksDurableOwner(t *testing.T) {
	now := time.Date(2026, 9, 14, 13, 30, 0, 0, time.UTC)
	store := newCoordinatorStore(now)
	owner := domain.SessionID("coord-a")
	store.sessions = []domain.SessionRecord{
		{ID: "root", ProjectID: "p", Kind: domain.KindOrchestrator, CreatedAt: now},
		{ID: "coord-a", ProjectID: "p", Kind: domain.KindCoordinator, CoordinatorScope: "auth", CreatedAt: now},
	}
	store.seedLink(&owner)
	c := NewCoordinator(CoordinatorDeps{Store: store, Delivery: &coordinatorDelivery{}, Now: func() time.Time { return now }})
	rec, ok, err := c.ResolveRemoteRecipient(context.Background(), domain.RemoteWorkerLink{
		HostID: "host", RemoteSessionID: "widgets-3", ProjectID: "p", OwnerSessionID: &owner,
	})
	if err != nil || !ok || rec.ID != "coord-a" {
		t.Fatalf("recipient = %+v ok=%v err=%v", rec, ok, err)
	}
	if len(store.ownerEvents) != 0 {
		t.Fatalf("live owner was repaired: %#v", store.ownerEvents)
	}
}

func TestResolveRemoteRecipientRepairsATerminatedOwner(t *testing.T) {
	now := time.Date(2026, 9, 14, 13, 35, 0, 0, time.UTC)
	store := newCoordinatorStore(now)
	owner := domain.SessionID("coord-gone")
	store.sessions = []domain.SessionRecord{
		{ID: "root", ProjectID: "p", Kind: domain.KindOrchestrator, CreatedAt: now},
		{ID: "coord-gone", ProjectID: "p", Kind: domain.KindCoordinator, CoordinatorScope: "auth", IsTerminated: true, CreatedAt: now},
	}
	store.seedLink(&owner)
	c := NewCoordinator(CoordinatorDeps{Store: store, Delivery: &coordinatorDelivery{}, Now: func() time.Time { return now }})
	rec, ok, err := c.ResolveRemoteRecipient(context.Background(), domain.RemoteWorkerLink{
		HostID: "host", RemoteSessionID: "widgets-3", ProjectID: "p", OwnerSessionID: &owner,
	})
	if err != nil || !ok || rec.ID != "root" {
		t.Fatalf("recipient = %+v ok=%v err=%v", rec, ok, err)
	}
	if len(store.ownerEvents) != 1 || store.ownerEvents[0].reason != domain.OwnershipOwnerNotLiveReconciled || store.ownerEvents[0].owner != nil {
		t.Fatalf("owner repair = %#v", store.ownerEvents)
	}
}

func TestResolveRemoteRecipientRepairsAnOwnerFromAnotherProject(t *testing.T) {
	now := time.Date(2026, 9, 14, 13, 40, 0, 0, time.UTC)
	store := newCoordinatorStore(now)
	owner := domain.SessionID("coord-other")
	store.sessions = []domain.SessionRecord{
		{ID: "root", ProjectID: "p", Kind: domain.KindOrchestrator, CreatedAt: now},
		{ID: "coord-other", ProjectID: "other", Kind: domain.KindCoordinator, CoordinatorScope: "docs", CreatedAt: now},
	}
	store.seedLink(&owner)
	c := NewCoordinator(CoordinatorDeps{Store: store, Delivery: &coordinatorDelivery{}, Now: func() time.Time { return now }})
	rec, ok, err := c.ResolveRemoteRecipient(context.Background(), domain.RemoteWorkerLink{
		HostID: "host", RemoteSessionID: "widgets-3", ProjectID: "p", OwnerSessionID: &owner,
	})
	if err != nil || !ok || rec.ID != "root" {
		t.Fatalf("recipient = %+v ok=%v err=%v", rec, ok, err)
	}
	if len(store.ownerEvents) != 1 || store.ownerEvents[0].reason != domain.OwnershipOwnerNotLiveReconciled {
		t.Fatalf("owner repair = %#v", store.ownerEvents)
	}
}

type coordinatorStore struct {
	mu          sync.Mutex
	now         time.Time
	reports     []domain.ReportRecord
	sessions    []domain.SessionRecord
	interrupts  map[domain.SessionID]time.Time
	acked       int
	ownerEvents []coordinatorOwnerEvent
	links       map[string]domain.RemoteWorkerLink
	// merges holds merge commit SHAs already claimed for announcement.
	merges    map[string]bool
	createErr error
	// beforeRemoteClear runs before a remote compare-and-clear reads the link.
	beforeRemoteClear func()
	// claims counts batch claims so a regression can bound a spin instead of
	// hanging: over the limit the store fails the claim.
	claims     int
	claimLimit int
}

type coordinatorOwnerEvent struct {
	session domain.SessionID
	owner   *domain.SessionID
	reason  domain.OwnershipEventReason
}

func newCoordinatorStore(now time.Time) *coordinatorStore {
	return &coordinatorStore{now: now, interrupts: map[domain.SessionID]time.Time{}}
}

func reportAt(id string, worker domain.SessionID, created, available time.Time, note string) domain.ReportRecord {
	return domain.ReportRecord{ID: id, SessionID: worker, ProjectID: "p", State: domain.ReportCheckpoint, Note: note, CreatedAt: created, AvailableAt: available, DeliveryState: domain.ReportPending, RepeatCount: 1}
}

func (s *coordinatorStore) add(records ...domain.ReportRecord) {
	s.reports = append(s.reports, records...)
}
func (s *coordinatorStore) ListSessions(context.Context, domain.ProjectID) ([]domain.SessionRecord, error) {
	return append([]domain.SessionRecord(nil), s.sessions...), nil
}
func (s *coordinatorStore) GetSession(_ context.Context, id domain.SessionID) (domain.SessionRecord, bool, error) {
	for _, rec := range s.sessions {
		if rec.ID == id {
			return rec, true, nil
		}
	}
	return domain.SessionRecord{}, false, nil
}
func (s *coordinatorStore) ListPendingReportSchedule(_ context.Context, limit int64) ([]domain.ReportRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var pending []domain.ReportRecord
	for _, rec := range s.reports {
		if rec.DeliveryState == domain.ReportPending {
			pending = append(pending, rec)
		}
	}
	sortReports(pending)
	if limit > 0 && int64(len(pending)) > limit {
		pending = pending[:limit]
	}
	return pending, nil
}
func (s *coordinatorStore) effectiveOwner(sessionID domain.SessionID, root domain.SessionID) domain.SessionID {
	for _, rec := range s.sessions {
		if rec.ID == sessionID {
			if rec.OwnerSessionID != nil {
				return *rec.OwnerSessionID
			}
			return root
		}
	}
	return root
}

func (s *coordinatorStore) claimable(sessionID, root, recipient domain.SessionID, exited []domain.SessionID) bool {
	owner := s.effectiveOwner(sessionID, root)
	if len(exited) > 0 && exited[0] != "" {
		return owner == exited[0]
	}
	return owner == recipient
}

func (s *coordinatorStore) ClaimPendingReportBatch(_ context.Context, project domain.ProjectID, recipient, root domain.SessionID, token string, at time.Time, exited ...domain.SessionID) ([]domain.ReportRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.claims++
	if s.claimLimit > 0 && s.claims > s.claimLimit {
		return nil, errors.New("claim limit reached")
	}
	batchID, fresh := "", false
	for i := range s.reports {
		rec := &s.reports[i]
		if rec.ProjectID == project && rec.DeliveryState == domain.ReportPending && s.claimable(rec.SessionID, root, recipient, exited) {
			batchID = rec.DeliveryBatchID
			if batchID == "" {
				batchID, fresh = "report-batch:"+rec.ID, true
			}
			break
		}
	}
	if batchID == "" {
		return nil, nil
	}
	var claimed []domain.ReportRecord
	for i := range s.reports {
		rec := &s.reports[i]
		if rec.ProjectID != project || rec.DeliveryState != domain.ReportPending || !s.claimable(rec.SessionID, root, recipient, exited) {
			continue
		}
		// Like the durable store, only unbatched reports join a fresh batch.
		if rec.DeliveryBatchID != batchID && (!fresh || rec.DeliveryBatchID != "") {
			continue
		}
		rec.DeliveryBatchID = batchID
		rec.DeliveryState, rec.ClaimToken, rec.ClaimedAt = domain.ReportClaimed, token, at
		rec.DeliveryOwnerID = string(recipient)
		claimed = append(claimed, *rec)
	}
	sortReports(claimed)
	return claimed, nil
}
func (s *coordinatorStore) AcknowledgeReportBatch(_ context.Context, project domain.ProjectID, token string, at time.Time) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	count := 0
	for i := range s.reports {
		if s.reports[i].ProjectID == project && s.reports[i].DeliveryState == domain.ReportClaimed && s.reports[i].ClaimToken == token {
			s.reports[i].DeliveryState, s.reports[i].AcknowledgedAt = domain.ReportAcknowledged, at
			count++
		}
	}
	s.acked += count
	return count, nil
}
func (s *coordinatorStore) ReleaseReportBatch(_ context.Context, project domain.ProjectID, token string, _ time.Time, _ string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	count := 0
	for i := range s.reports {
		if s.reports[i].ProjectID == project && s.reports[i].DeliveryState == domain.ReportClaimed && s.reports[i].ClaimToken == token {
			s.reports[i].DeliveryState, s.reports[i].ClaimToken = domain.ReportPending, ""
			count++
		}
	}
	return count, nil
}
func (s *coordinatorStore) DeferReportBatch(ctx context.Context, project domain.ProjectID, token string, at time.Time, reason string) (int, error) {
	count, err := s.ReleaseReportBatch(ctx, project, token, at, reason)
	if err != nil {
		return count, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.reports {
		if s.reports[i].ProjectID == project && s.reports[i].DeliveryState == domain.ReportPending && s.reports[i].DeliveryBatchID != "" && s.reports[i].AvailableAt.Before(at) {
			s.reports[i].AvailableAt = at
		}
	}
	return count, nil
}
func (s *coordinatorStore) ReleaseReportInterrupt(_ context.Context, worker domain.SessionID, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if last, ok := s.interrupts[worker]; ok && last.Equal(at) {
		delete(s.interrupts, worker)
	}
	return nil
}

func (s *coordinatorStore) AcquireReportInterrupt(_ context.Context, worker domain.SessionID, at time.Time, window time.Duration) (bool, time.Time, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	last, ok := s.interrupts[worker]
	if ok && last.After(at.Add(-window)) {
		return false, last.Add(window), nil
	}
	s.interrupts[worker] = at
	return true, at, nil
}

func sameOwnerPtr(a, b *domain.SessionID) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

func (s *coordinatorStore) ClearSessionOwnerIf(_ context.Context, id, expected domain.SessionID, reason domain.OwnershipEventReason, _ time.Time) (domain.SessionRecord, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.sessions {
		if s.sessions[i].ID != id {
			continue
		}
		if !sameOwnerPtr(s.sessions[i].OwnerSessionID, &expected) {
			return s.sessions[i], false, nil
		}
		s.sessions[i].OwnerSessionID = nil
		s.ownerEvents = append(s.ownerEvents, coordinatorOwnerEvent{session: id, reason: reason})
		return s.sessions[i], true, nil
	}
	return domain.SessionRecord{}, false, nil
}

func (s *coordinatorStore) GetRemoteWorkerLink(_ context.Context, hostID string, remote domain.SessionID) (domain.RemoteWorkerLink, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	link, ok := s.links[hostID+"/"+string(remote)]
	return link, ok, nil
}

func (s *coordinatorStore) ClaimPRMergeAnnouncement(_ context.Context, sha, _ string, _ domain.SessionID, _ time.Time) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.merges == nil {
		s.merges = map[string]bool{}
	}
	if s.merges[sha] {
		return false, nil
	}
	s.merges[sha] = true
	return true, nil
}

func (s *coordinatorStore) ReleasePRMergeAnnouncement(_ context.Context, sha string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.merges, sha)
	return nil
}

func (s *coordinatorStore) ClearRemoteWorkerOwnerIf(_ context.Context, hostID string, remote, expected domain.SessionID, reason domain.OwnershipEventReason, _ time.Time) (domain.RemoteWorkerLink, bool, error) {
	if s.beforeRemoteClear != nil {
		s.beforeRemoteClear()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	key := hostID + "/" + string(remote)
	link := s.links[key]
	if !sameOwnerPtr(link.OwnerSessionID, &expected) {
		return link, false, nil
	}
	link.OwnerSessionID = nil
	s.links[key] = link
	s.ownerEvents = append(s.ownerEvents, coordinatorOwnerEvent{session: remote, reason: reason})
	return link, true, nil
}

// seedLink stores a remote link whose durable owner is owner.
func (s *coordinatorStore) seedLink(owner *domain.SessionID) {
	s.links = map[string]domain.RemoteWorkerLink{"host/widgets-3": {
		HostID: "host", RemoteSessionID: "widgets-3", ProjectID: "p", OwnerSessionID: owner,
	}}
}

func sortReports(records []domain.ReportRecord) {
	for i := 1; i < len(records); i++ {
		for j := i; j > 0 && (records[j].AvailableAt.Before(records[j-1].AvailableAt) || (records[j].AvailableAt.Equal(records[j-1].AvailableAt) && records[j].ID < records[j-1].ID)); j-- {
			records[j], records[j-1] = records[j-1], records[j]
		}
	}
}

type coordinatorSubmission struct {
	target       domain.SessionID
	message, key string
}
type coordinatorDelivery struct {
	submissions  []coordinatorSubmission
	interrupts   []domain.SessionID
	submitErr    error
	interruptErr error
	submitHook   func(context.Context) error
	attempts     int
}

func (d *coordinatorDelivery) Submit(ctx context.Context, target domain.SessionID, message, key string) error {
	d.attempts++
	if d.submitHook != nil {
		if err := d.submitHook(ctx); err != nil {
			return err
		}
	}
	if d.submitErr != nil {
		return d.submitErr
	}
	d.submissions = append(d.submissions, coordinatorSubmission{target, message, key})
	return nil
}
func (d *coordinatorDelivery) Interrupt(_ context.Context, target domain.SessionID) error {
	d.interrupts = append(d.interrupts, target)
	return d.interruptErr
}

func TestCoordinatorAcceptanceTimeoutConsumesTheBatchOnce(t *testing.T) {
	now := time.Now()
	store := newCoordinatorStore(now)
	store.sessions = []domain.SessionRecord{{ID: "orch", ProjectID: "p", Kind: domain.KindOrchestrator}}
	store.add(reportAt("one", "worker", now, now, "finished"))
	d := &coordinatorDelivery{submitErr: domain.ErrSemanticAcceptanceTimeout}
	c := NewCoordinator(CoordinatorDeps{Store: store, Delivery: d, Now: func() time.Time { return now }})
	if _, err := c.RunDue(context.Background()); err != nil {
		t.Fatal(err)
	}
	// The prompt was written before the acceptance timeout: deliver it once.
	if d.attempts != 1 || store.acked != 1 {
		t.Fatalf("attempts=%d acknowledged=%d, want one consumed delivery", d.attempts, store.acked)
	}
	// Neither a late hook nor a restarted daemon resends the consumed batch.
	d.submitErr = nil
	for i := 0; i < 5; i++ {
		now = now.Add(time.Hour)
		restarted := NewCoordinator(CoordinatorDeps{Store: store, Delivery: d, Now: func() time.Time { return now }})
		if _, err := restarted.RunDue(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if d.attempts != 1 || len(d.submissions) != 0 {
		t.Fatalf("attempts=%d submissions=%d, want no replay of a consumed batch", d.attempts, len(d.submissions))
	}
}

// A flush that acknowledges nothing must not be retried in the same pass:
// a terminated worker's rate-limited stuck batch used to be claimed forever.
func TestCoordinatorTerminatedStuckFlushDoesNotSpin(t *testing.T) {
	now := time.Date(2026, 9, 14, 15, 0, 0, 0, time.UTC)
	store := newCoordinatorStore(now)
	store.sessions = []domain.SessionRecord{
		{ID: "orch", ProjectID: "p", Kind: domain.KindOrchestrator},
		{ID: "ended", ProjectID: "p", Kind: domain.KindWorker, IsTerminated: true},
	}
	stuck := reportAt("stuck", "ended", now.Add(-time.Minute), now, "blocked")
	stuck.State = domain.ReportStuck
	store.add(stuck)
	// The interrupt window from an earlier interruption is still open.
	store.interrupts["ended"] = now
	store.claimLimit = 3
	delivery := &coordinatorDelivery{}
	c := NewCoordinator(CoordinatorDeps{Store: store, Delivery: delivery, Now: func() time.Time { return now }})

	delay, err := c.RunDue(context.Background())
	if err != nil {
		t.Fatalf("the deferred flush spun: %v", err)
	}
	if delay != ReportInterruptWindow {
		t.Fatalf("delay = %v, want the deferral deadline %v", delay, ReportInterruptWindow)
	}
	if store.claims != 1 || len(delivery.submissions) != 0 {
		t.Fatalf("claims=%d submissions=%d, want one deferred claim and no delivery", store.claims, len(delivery.submissions))
	}
}

func TestCoordinatorFlushesATerminatedWorkersQueueOnce(t *testing.T) {
	now := time.Date(2026, 9, 14, 14, 0, 0, 0, time.UTC)
	store := newCoordinatorStore(now)
	store.sessions = []domain.SessionRecord{
		{ID: "orch", ProjectID: "p", Kind: domain.KindOrchestrator},
		{ID: "ended", ProjectID: "p", Kind: domain.KindWorker, IsTerminated: true},
		{ID: "live", ProjectID: "p", Kind: domain.KindWorker},
	}
	store.add(reportAt("late", "ended", now, now.Add(time.Hour), "final head"))
	delivery := &coordinatorDelivery{}
	c := NewCoordinator(CoordinatorDeps{Store: store, Delivery: delivery, Now: func() time.Time { return now }})
	if _, err := c.RunDue(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(delivery.submissions) != 1 || !strings.Contains(delivery.submissions[0].message, "final head") || store.acked != 1 {
		t.Fatalf("terminated flush = %#v acked=%d", delivery.submissions, store.acked)
	}
	// A live worker keeps its normal deadline, and later passes send nothing.
	store.add(reportAt("live-one", "live", now, now.Add(time.Hour), "live head"))
	if delay, err := c.RunDue(context.Background()); err != nil || delay != time.Hour {
		t.Fatalf("live delay = %v, err = %v", delay, err)
	}
	for i := 0; i < 3; i++ {
		now = now.Add(time.Minute)
		if _, err := c.RunDue(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if len(delivery.submissions) != 1 {
		t.Fatalf("a terminated session kept producing deliveries: %#v", delivery.submissions)
	}
	now = now.Add(time.Hour)
	if _, err := c.RunDue(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(delivery.submissions) != 2 || !strings.Contains(delivery.submissions[1].message, "live head") {
		t.Fatalf("live delivery = %#v", delivery.submissions)
	}
}

// runLoopUntilAcked drives the real Start loop, with no Wake, until want
// reports are acknowledged, then stops it and returns the submissions.
func runLoopUntilAcked(t *testing.T, store *coordinatorStore, delivery *coordinatorDelivery, now time.Time, want int) []coordinatorSubmission {
	t.Helper()
	c := NewCoordinator(CoordinatorDeps{Store: store, Delivery: delivery, Now: func() time.Time { return now }, RetryDelay: 5 * time.Millisecond})
	ctx, cancel := context.WithCancel(context.Background())
	done := c.Start(ctx)
	deadline := time.Now().Add(5 * time.Second)
	for {
		store.mu.Lock()
		acked := store.acked
		store.mu.Unlock()
		if acked >= want || time.Now().After(deadline) {
			break
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	<-done
	return delivery.submissions
}

func TestCoordinatorLoopDeliversUnbatchedReportsBehindAReleasedBatch(t *testing.T) {
	now := time.Date(2026, 9, 14, 16, 0, 0, 0, time.UTC)
	store := newCoordinatorStore(now)
	store.sessions = []domain.SessionRecord{{ID: "orch", ProjectID: "p", Kind: domain.KindOrchestrator}}
	released := reportAt("released", "worker", now.Add(-time.Minute), now, "released head")
	released.DeliveryBatchID = "report-batch:released"
	store.add(released, reportAt("fresh", "worker", now, now, "fresh head"))

	submissions := runLoopUntilAcked(t, store, &coordinatorDelivery{}, now, 2)
	if len(submissions) != 2 || !strings.Contains(submissions[0].message, "released head") || !strings.Contains(submissions[1].message, "fresh head") {
		t.Fatalf("submissions = %#v, want both reports without a wake-up", submissions)
	}
}

func TestCoordinatorLoopDeliversReassignedWorkersOldBatchToTheNewOwner(t *testing.T) {
	now := time.Date(2026, 9, 14, 16, 30, 0, 0, time.UTC)
	store := newCoordinatorStore(now)
	owner := domain.SessionID("coord")
	store.sessions = []domain.SessionRecord{
		{ID: "root", ProjectID: "p", Kind: domain.KindOrchestrator, Mode: domain.SessionModeChat, CreatedAt: now},
		{ID: "coord", ProjectID: "p", Kind: domain.KindCoordinator, CoordinatorScope: "auth", CreatedAt: now},
		{ID: "moved", ProjectID: "p", Kind: domain.KindWorker, OwnerSessionID: &owner},
		{ID: "native", ProjectID: "p", Kind: domain.KindWorker, OwnerSessionID: &owner},
	}
	old := reportAt("old", "moved", now.Add(-time.Minute), now, "carries old batch")
	old.DeliveryBatchID = "report-batch:previous-group"
	store.add(old, reportAt("new", "native", now, now, "unbatched"))

	submissions := runLoopUntilAcked(t, store, &coordinatorDelivery{}, now, 2)
	if len(submissions) != 2 || submissions[0].target != "coord" || submissions[1].target != "coord" {
		t.Fatalf("submissions = %#v, want both delivered to the new owner", submissions)
	}
}

func TestCoordinatorRunDueRetriesWhileADueReportRemains(t *testing.T) {
	now := time.Date(2026, 9, 14, 17, 0, 0, 0, time.UTC)
	store := newCoordinatorStore(now)
	store.sessions = []domain.SessionRecord{{ID: "orch", ProjectID: "p", Kind: domain.KindOrchestrator}}
	released := reportAt("released", "worker", now.Add(-time.Minute), now, "released head")
	released.DeliveryBatchID = "report-batch:released"
	store.add(released, reportAt("fresh", "worker", now, now, "fresh head"))
	c := NewCoordinator(CoordinatorDeps{Store: store, Delivery: &coordinatorDelivery{}, Now: func() time.Time { return now }, RetryDelay: time.Second})

	if delay, err := c.RunDue(context.Background()); err != nil || delay != time.Second {
		t.Fatalf("first pass delay = %v, err = %v, want the retry delay", delay, err)
	}
	if delay, err := c.RunDue(context.Background()); err != nil || delay >= 0 {
		t.Fatalf("second pass delay = %v, err = %v, want idle", delay, err)
	}
}

func TestResolveRemoteRecipientUsesTheCurrentOwnerNotTheSnapshot(t *testing.T) {
	now := time.Date(2026, 9, 14, 13, 45, 0, 0, time.UTC)
	store := newCoordinatorStore(now)
	gone, current := domain.SessionID("coord-gone"), domain.SessionID("coord-new")
	store.sessions = []domain.SessionRecord{
		{ID: "root", ProjectID: "p", Kind: domain.KindOrchestrator, CreatedAt: now},
		{ID: "coord-gone", ProjectID: "p", Kind: domain.KindCoordinator, CoordinatorScope: "a", IsTerminated: true, CreatedAt: now},
		{ID: "coord-new", ProjectID: "p", Kind: domain.KindCoordinator, CoordinatorScope: "b", CreatedAt: now},
	}
	store.seedLink(&current)
	c := NewCoordinator(CoordinatorDeps{Store: store, Delivery: &coordinatorDelivery{}, Now: func() time.Time { return now }})
	rec, ok, err := c.ResolveRemoteRecipient(context.Background(), domain.RemoteWorkerLink{
		HostID: "host", RemoteSessionID: "widgets-3", ProjectID: "p", OwnerSessionID: &gone,
	})
	if err != nil || !ok || rec.ID != "coord-new" {
		t.Fatalf("recipient = %+v ok=%v err=%v, want coord-new", rec, ok, err)
	}
	if len(store.ownerEvents) != 0 || *store.links["host/widgets-3"].OwnerSessionID != "coord-new" {
		t.Fatalf("reassignment was disturbed: events=%#v link=%+v", store.ownerEvents, store.links)
	}
}

func TestResolveRemoteRecipientKeepsAReassignmentMadeDuringRepair(t *testing.T) {
	now := time.Date(2026, 9, 14, 13, 50, 0, 0, time.UTC)
	store := newCoordinatorStore(now)
	gone, current := domain.SessionID("coord-gone"), domain.SessionID("coord-new")
	store.sessions = []domain.SessionRecord{
		{ID: "root", ProjectID: "p", Kind: domain.KindOrchestrator, CreatedAt: now},
		{ID: "coord-gone", ProjectID: "p", Kind: domain.KindCoordinator, CoordinatorScope: "a", IsTerminated: true, CreatedAt: now},
		{ID: "coord-new", ProjectID: "p", Kind: domain.KindCoordinator, CoordinatorScope: "b", CreatedAt: now},
	}
	store.seedLink(&gone)
	store.beforeRemoteClear = func() {
		store.mu.Lock()
		defer store.mu.Unlock()
		link := store.links["host/widgets-3"]
		link.OwnerSessionID = &current
		store.links["host/widgets-3"] = link
		store.beforeRemoteClear = nil
	}
	c := NewCoordinator(CoordinatorDeps{Store: store, Delivery: &coordinatorDelivery{}, Now: func() time.Time { return now }})
	rec, ok, err := c.ResolveRemoteRecipient(context.Background(), domain.RemoteWorkerLink{
		HostID: "host", RemoteSessionID: "widgets-3", ProjectID: "p", OwnerSessionID: &gone,
	})
	if err != nil || !ok || rec.ID != "coord-new" {
		t.Fatalf("recipient = %+v ok=%v err=%v, want coord-new", rec, ok, err)
	}
	if len(store.ownerEvents) != 0 || *store.links["host/widgets-3"].OwnerSessionID != "coord-new" {
		t.Fatalf("repair undid the reassignment: events=%#v link=%+v", store.ownerEvents, store.links)
	}
}
