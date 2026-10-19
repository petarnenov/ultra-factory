package report_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/apierr"
	reportsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/report"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/sqlitetest"
)

type summaryFixture struct {
	t        *testing.T
	store    *sqlite.Store
	clock    time.Time
	delivery *summaryDelivery
	svc      *reportsvc.SummaryService
	coord    *reportsvc.Coordinator
	root     domain.SessionRecord
	coordA   domain.SessionRecord
	coordB   domain.SessionRecord
	workerA1 domain.SessionRecord
	workerA2 domain.SessionRecord
	workerB  domain.SessionRecord
	loose    domain.SessionRecord
}

type summarySubmission struct {
	target  domain.SessionID
	message string
	key     string
}

type summaryDelivery struct {
	submissions []summarySubmission
	interrupts  []domain.SessionID
	submitErr   error
}

func (d *summaryDelivery) Submit(_ context.Context, target domain.SessionID, message, key string) error {
	if d.submitErr != nil {
		return d.submitErr
	}
	d.submissions = append(d.submissions, summarySubmission{target, message, key})
	return nil
}

func (d *summaryDelivery) Interrupt(_ context.Context, target domain.SessionID) error {
	d.interrupts = append(d.interrupts, target)
	return nil
}

func newSummaryFixture(t *testing.T) *summaryFixture {
	t.Helper()
	ctx := context.Background()
	f := &summaryFixture{t: t, store: sqlitetest.MustOpen(t), clock: time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC), delivery: &summaryDelivery{}}
	if err := f.store.UpsertProject(ctx, domain.ProjectRecord{ID: "p", Path: "/tmp/p", RegisteredAt: f.clock}); err != nil {
		t.Fatal(err)
	}
	create := func(kind domain.SessionKind, scope string, owner *domain.SessionID, name string) domain.SessionRecord {
		rec, err := f.store.CreateSession(ctx, domain.SessionRecord{
			ProjectID: "p", Kind: kind, Harness: domain.HarnessClaudeCode, CoordinatorScope: scope, OwnerSessionID: owner,
			DisplayName: name, Activity: domain.Activity{State: domain.ActivityActive, LastActivityAt: f.clock},
			CreatedAt: f.clock, UpdatedAt: f.clock,
		})
		if err != nil {
			t.Fatal(err)
		}
		return rec
	}
	f.root = create(domain.KindOrchestrator, "", nil, "Root")
	f.coordA = create(domain.KindCoordinator, "auth area", nil, "Coordinator A")
	f.coordB = create(domain.KindCoordinator, "billing area", nil, "Coordinator B")
	f.workerA1 = create(domain.KindWorker, "", &f.coordA.ID, "Worker A1")
	f.workerA2 = create(domain.KindWorker, "", &f.coordA.ID, "Worker A2")
	f.workerB = create(domain.KindWorker, "", &f.coordB.ID, "Worker B")
	f.loose = create(domain.KindWorker, "", nil, "Root-owned worker")
	f.svc = reportsvc.NewSummaryService(reportsvc.SummaryDeps{Store: f.store, Now: func() time.Time { return f.clock }})
	n := 0
	f.coord = reportsvc.NewCoordinator(reportsvc.CoordinatorDeps{
		Store: f.store, Summaries: f.store, Delivery: f.delivery, Now: func() time.Time { return f.clock },
		NewToken: func() string { n++; return "token-" + string(rune('a'+n)) }, RetryDelay: time.Second,
	})
	return f
}

func (f *summaryFixture) submit(coordinator domain.SessionID, key string, state domain.CoordinatorSummaryState, workers ...domain.SessionID) domain.CoordinatorSummary {
	f.t.Helper()
	rec, _, err := f.svc.Submit(context.Background(), reportsvc.SummaryInput{
		CoordinatorSessionID: coordinator, SubmissionKey: key, State: state, WorkerSessionIDs: workers, Result: "result " + key,
	})
	if err != nil {
		f.t.Fatalf("submit %s: %v", key, err)
	}
	return rec
}

func (f *summaryFixture) escalate(coordinator domain.SessionID, key string, reason domain.EscalationReason, workers ...domain.SessionID) domain.CoordinatorSummary {
	f.t.Helper()
	rec, _, err := f.svc.Submit(context.Background(), reportsvc.SummaryInput{
		CoordinatorSessionID: coordinator, SubmissionKey: key, State: domain.SummaryBlocked, WorkerSessionIDs: workers,
		Result: "blocked " + key, EscalationReason: reason, EscalationNote: "note " + key,
	})
	if err != nil {
		f.t.Fatalf("escalate %s: %v", key, err)
	}
	return rec
}

func (f *summaryFixture) run() {
	f.t.Helper()
	if _, err := f.coord.RunDue(context.Background()); err != nil {
		f.t.Fatalf("RunDue: %v", err)
	}
}

func (f *summaryFixture) history(coordinator domain.SessionID) []domain.CoordinatorSummary {
	f.t.Helper()
	recs, err := f.svc.List(context.Background(), coordinator, 50)
	if err != nil {
		f.t.Fatal(err)
	}
	return recs
}

func apiCode(err error) string {
	var apiErr *apierr.Error
	if errors.As(err, &apiErr) {
		return apiErr.Code
	}
	return ""
}

func TestSummaryIsDistinguishableAndLinkedToItsWorkers(t *testing.T) {
	f := newSummaryFixture(t)
	f.submit(f.coordA.ID, "k1", domain.SummaryWorking, f.workerA1.ID, f.workerA2.ID)
	f.clock = f.clock.Add(domain.CoordinatorSummaryBatchWindow)
	f.run()

	if len(f.delivery.submissions) != 1 {
		t.Fatalf("submissions = %#v, want one root turn", f.delivery.submissions)
	}
	got := f.delivery.submissions[0]
	if got.target != f.root.ID {
		t.Fatalf("recipient = %s, want root %s", got.target, f.root.ID)
	}
	for _, want := range []string{
		"Coordinator summaries", "not worker reports", "[working]", "scope: auth area", "result k1",
		"[Coordinator A](ao://sessions/p/" + string(f.coordA.ID) + ")",
		"[Worker A1](ao://sessions/p/" + string(f.workerA1.ID) + ")",
		"[Worker A2](ao://sessions/p/" + string(f.workerA2.ID) + ")",
	} {
		if !strings.Contains(got.message, want) {
			t.Fatalf("message missing %q:\n%s", want, got.message)
		}
	}
	if strings.Contains(got.message, "Reports since your previous turn") {
		t.Fatalf("summary rendered as a worker report:\n%s", got.message)
	}
	hist := f.history(f.coordA.ID)
	if len(hist) != 1 || hist[0].DeliveryState != domain.ReportAcknowledged || len(hist[0].WorkerSessionIDs) != 2 || hist[0].WorkerSessionIDs[0] != f.workerA1.ID {
		t.Fatalf("history = %+v, want one acknowledged summary linked to both workers in order", hist)
	}
	latest, ok, err := f.store.GetLatestAcceptedCoordinatorSummary(context.Background(), f.coordA.ID)
	if err != nil || !ok || latest.ID != hist[0].ID {
		t.Fatalf("latest accepted = %+v ok=%v err=%v", latest, ok, err)
	}
}

func TestSummaryDoesNotTouchWorkerReports(t *testing.T) {
	f := newSummaryFixture(t)
	ctx := context.Background()
	rep, err := f.store.CreateReport(ctx, domain.ReportRecord{
		ID: "rpt_1", SessionID: f.workerA1.ID, ProjectID: "p", State: domain.ReportCheckpoint, Note: "raw worker note",
		CreatedAt: f.clock, DeliveryState: domain.ReportPending, AvailableAt: f.clock.Add(time.Hour), RepeatCount: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	f.submit(f.coordA.ID, "k1", domain.SummaryDone, f.workerA1.ID)
	f.clock = f.clock.Add(domain.CoordinatorSummaryBatchWindow)
	f.run()

	got, ok, err := f.store.GetReport(ctx, rep.ID)
	if err != nil || !ok || got.DeliveryState != domain.ReportPending || got.Note != "raw worker note" || got.DeliveryOwnerID != "" {
		t.Fatalf("worker report changed by a summary: %+v ok=%v err=%v", got, ok, err)
	}
	if len(f.delivery.submissions) != 1 || strings.Contains(f.delivery.submissions[0].message, "raw worker note") {
		t.Fatalf("summary forwarded the raw worker report: %#v", f.delivery.submissions)
	}
}

func TestSummarySubmissionRules(t *testing.T) {
	f := newSummaryFixture(t)
	ctx := context.Background()
	cases := []struct {
		name string
		in   reportsvc.SummaryInput
		code string
	}{
		{"worker cannot submit", reportsvc.SummaryInput{CoordinatorSessionID: f.workerA1.ID, SubmissionKey: "k", State: domain.SummaryWorking}, "SUMMARY_COORDINATOR_REQUIRED"},
		{"root cannot submit", reportsvc.SummaryInput{CoordinatorSessionID: f.root.ID, SubmissionKey: "k", State: domain.SummaryWorking}, "SUMMARY_COORDINATOR_REQUIRED"},
		{"unknown session", reportsvc.SummaryInput{CoordinatorSessionID: "nope", SubmissionKey: "k", State: domain.SummaryWorking}, "SESSION_NOT_FOUND"},
		{"submission key required", reportsvc.SummaryInput{CoordinatorSessionID: f.coordA.ID, State: domain.SummaryWorking}, "INVALID_SUMMARY"},
		{"state required", reportsvc.SummaryInput{CoordinatorSessionID: f.coordA.ID, SubmissionKey: "k"}, "INVALID_SUMMARY"},
		{"worker owned by another coordinator", reportsvc.SummaryInput{CoordinatorSessionID: f.coordA.ID, SubmissionKey: "k", State: domain.SummaryWorking, WorkerSessionIDs: []domain.SessionID{f.workerB.ID}}, "SUMMARY_WORKER_NOT_OWNED"},
		{"root-owned worker", reportsvc.SummaryInput{CoordinatorSessionID: f.coordA.ID, SubmissionKey: "k", State: domain.SummaryWorking, WorkerSessionIDs: []domain.SessionID{f.loose.ID}}, "SUMMARY_WORKER_NOT_OWNED"},
		{"too many workers", reportsvc.SummaryInput{CoordinatorSessionID: f.coordA.ID, SubmissionKey: "k", State: domain.SummaryWorking, WorkerSessionIDs: []domain.SessionID{"a", "b", "c", "d"}}, "INVALID_SUMMARY"},
		{"result too long", reportsvc.SummaryInput{CoordinatorSessionID: f.coordA.ID, SubmissionKey: "k", State: domain.SummaryWorking, Result: strings.Repeat("x", 2001)}, "INVALID_SUMMARY"},
		{"scope too long", reportsvc.SummaryInput{CoordinatorSessionID: f.coordA.ID, SubmissionKey: "k", State: domain.SummaryWorking, Scope: strings.Repeat("x", 201)}, "INVALID_SUMMARY"},
		{"unknown escalation reason", reportsvc.SummaryInput{CoordinatorSessionID: f.coordA.ID, SubmissionKey: "k", State: domain.SummaryBlocked, EscalationReason: "bored"}, "INVALID_SUMMARY"},
		{"escalation needs blocked", reportsvc.SummaryInput{CoordinatorSessionID: f.coordA.ID, SubmissionKey: "k", State: domain.SummaryWorking, EscalationReason: domain.EscalationWorkerFailed}, "INVALID_SUMMARY"},
		{"other needs a note", reportsvc.SummaryInput{CoordinatorSessionID: f.coordA.ID, SubmissionKey: "k", State: domain.SummaryBlocked, EscalationReason: domain.EscalationOther}, "INVALID_SUMMARY"},
		{"note without reason", reportsvc.SummaryInput{CoordinatorSessionID: f.coordA.ID, SubmissionKey: "k", State: domain.SummaryBlocked, EscalationNote: "why"}, "INVALID_SUMMARY"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, _, err := f.svc.Submit(ctx, tc.in); apiCode(err) != tc.code {
				t.Fatalf("code = %q (err %v), want %q", apiCode(err), err, tc.code)
			}
		})
	}
	if hist := f.history(f.coordA.ID); len(hist) != 0 {
		t.Fatalf("rejected submissions persisted: %+v", hist)
	}
	rec := f.submit(f.coordA.ID, "defaults", domain.SummaryPlanning)
	if rec.Scope != "auth area" {
		t.Fatalf("scope = %q, want the coordinator's scope", rec.Scope)
	}
}

func TestSummaryDuplicateSubmissionReturnsExistingAndEnqueuesNoSecondDelivery(t *testing.T) {
	f := newSummaryFixture(t)
	first, created, err := f.svc.Submit(context.Background(), reportsvc.SummaryInput{CoordinatorSessionID: f.coordA.ID, SubmissionKey: "same", State: domain.SummaryWorking, Result: "one", WorkerSessionIDs: []domain.SessionID{f.workerA1.ID}})
	if err != nil || !created {
		t.Fatalf("first created=%v err=%v", created, err)
	}
	again, created, err := f.svc.Submit(context.Background(), reportsvc.SummaryInput{CoordinatorSessionID: f.coordA.ID, SubmissionKey: "same", State: domain.SummaryDone, Result: "different payload"})
	if err != nil || created || again.ID != first.ID || again.Result != "one" {
		t.Fatalf("duplicate = %+v created=%v err=%v, want the original", again, created, err)
	}
	f.clock = f.clock.Add(domain.CoordinatorSummaryBatchWindow)
	f.run()
	f.run()
	if len(f.delivery.submissions) != 1 || len(f.history(f.coordA.ID)) != 1 {
		t.Fatalf("submissions=%d history=%d, want exactly one", len(f.delivery.submissions), len(f.history(f.coordA.ID)))
	}
	// A replay after acknowledgement returns the original, delivers nothing.
	if _, created, err := f.svc.Submit(context.Background(), reportsvc.SummaryInput{CoordinatorSessionID: f.coordA.ID, SubmissionKey: "same", State: domain.SummaryWorking}); err != nil || created {
		t.Fatalf("replay after ack created=%v err=%v", created, err)
	}
	f.run()
	if len(f.delivery.submissions) != 1 {
		t.Fatalf("replay re-delivered: %d submissions", len(f.delivery.submissions))
	}
}

func TestSummarySurvivesCoordinatorTerminationAndRootAbsence(t *testing.T) {
	f := newSummaryFixture(t)
	ctx := context.Background()
	f.submit(f.coordA.ID, "k1", domain.SummaryDone, f.workerA1.ID)
	f.clock = f.clock.Add(domain.CoordinatorSummaryBatchWindow)

	// The root is gone and the coordinator is terminated before delivery.
	f.root.IsTerminated = true
	if err := f.store.UpdateSession(ctx, f.root); err != nil {
		t.Fatal(err)
	}
	f.coordA.IsTerminated = true
	if err := f.store.UpdateSession(ctx, f.coordA); err != nil {
		t.Fatal(err)
	}
	if delay, err := f.coord.RunDue(ctx); !errors.Is(err, reportsvc.ErrNoActiveOrchestrator) || delay != time.Second {
		t.Fatalf("no root: delay=%v err=%v, want retry with ErrNoActiveOrchestrator", delay, err)
	}
	if hist := f.history(f.coordA.ID); len(hist) != 1 || hist[0].DeliveryState != domain.ReportPending || hist[0].DeliveryAttempts != 0 {
		t.Fatalf("history = %+v, want a pending, unclaimed summary", hist)
	}

	replacement, err := f.store.CreateSession(ctx, domain.SessionRecord{
		ProjectID: "p", Kind: domain.KindOrchestrator, Harness: domain.HarnessClaudeCode, DisplayName: "New root",
		Activity: domain.Activity{State: domain.ActivityActive, LastActivityAt: f.clock}, CreatedAt: f.clock.Add(time.Second), UpdatedAt: f.clock,
	})
	if err != nil {
		t.Fatal(err)
	}
	f.run()
	if len(f.delivery.submissions) != 1 || f.delivery.submissions[0].target != replacement.ID {
		t.Fatalf("submissions = %#v, want delivery to the replacement root", f.delivery.submissions)
	}
	if hist := f.history(f.coordA.ID); hist[0].DeliveryState != domain.ReportAcknowledged || hist[0].DeliveryOwnerID != string(replacement.ID) {
		t.Fatalf("history = %+v, want acknowledged with the root frozen at claim", hist)
	}
	if _, _, err := f.svc.Submit(ctx, reportsvc.SummaryInput{CoordinatorSessionID: f.coordA.ID, SubmissionKey: "new", State: domain.SummaryWorking}); apiCode(err) != "SUMMARY_COORDINATOR_TERMINATED" {
		t.Fatalf("terminated coordinator submit err = %v", err)
	}
	if _, created, err := f.svc.Submit(ctx, reportsvc.SummaryInput{CoordinatorSessionID: f.coordA.ID, SubmissionKey: "k1", State: domain.SummaryWorking}); err != nil || created {
		t.Fatalf("replay by a terminated coordinator created=%v err=%v, want the original", created, err)
	}
}

func TestSummaryRetryReusesBatchIdentityAndAcknowledgesOnlyAfterAcceptance(t *testing.T) {
	f := newSummaryFixture(t)
	ctx := context.Background()
	f.submit(f.coordA.ID, "k1", domain.SummaryWorking)
	f.submit(f.coordA.ID, "k2", domain.SummaryWorking)
	f.clock = f.clock.Add(domain.CoordinatorSummaryBatchWindow)

	f.delivery.submitErr = errors.New("root pane unavailable")
	if _, err := f.coord.RunDue(ctx); err == nil {
		t.Fatal("expected the delivery failure")
	}
	for _, s := range f.history(f.coordA.ID) {
		if s.DeliveryState != domain.ReportPending || s.AcknowledgedAt != (time.Time{}) || s.LastError == "" || s.DeliveryAttempts != 1 {
			t.Fatalf("after failed delivery: %+v, want pending, unacknowledged, error recorded", s)
		}
	}
	// A third summary arrives while the first two are retried.
	f.submit(f.coordA.ID, "k3", domain.SummaryDone)

	f.delivery.submitErr = nil
	f.clock = f.clock.Add(domain.CoordinatorSummaryBatchWindow)
	f.run()
	if len(f.delivery.submissions) != 1 {
		t.Fatalf("submissions = %#v, want the retried batch", f.delivery.submissions)
	}
	retried := f.delivery.submissions[0]
	if strings.Contains(retried.message, "result k3") || !strings.Contains(retried.message, "result k1") || !strings.Contains(retried.message, "result k2") {
		t.Fatalf("retry changed the coalesced set:\n%s", retried.message)
	}
	f.run()
	if len(f.delivery.submissions) != 2 || !strings.Contains(f.delivery.submissions[1].message, "result k3") {
		t.Fatalf("submissions = %#v, want k3 as its own later batch", f.delivery.submissions)
	}
	if retried.key == f.delivery.submissions[1].key || !strings.HasPrefix(f.delivery.submissions[1].key, "coordinator-summary:") {
		t.Fatalf("keys %q and %q, want distinct stable identities", retried.key, f.delivery.submissions[1].key)
	}
	// The identical set must always produce the identical key.
	hist := f.history(f.coordA.ID)
	ids := []string{}
	for _, s := range hist {
		if s.DeliveryBatchID == retried.key {
			ids = append(ids, s.ID)
		}
	}
	if len(ids) != 2 || domain.CoordinatorSummaryBatchID(ids) != retried.key {
		t.Fatalf("batch %q members %v, want the hash of exactly the coalesced ids", retried.key, ids)
	}
}

func TestSummaryAcceptanceTimeoutDefersAndRestartRequeuesClaims(t *testing.T) {
	f := newSummaryFixture(t)
	ctx := context.Background()
	f.submit(f.coordA.ID, "k1", domain.SummaryWorking)
	f.clock = f.clock.Add(domain.CoordinatorSummaryBatchWindow)

	f.delivery.submitErr = domain.ErrSemanticAcceptanceTimeout
	if _, err := f.coord.RunDue(ctx); !errors.Is(err, domain.ErrSemanticAcceptanceTimeout) {
		t.Fatalf("err = %v, want the acceptance timeout", err)
	}
	f.delivery.submitErr = nil
	f.clock = f.clock.Add(time.Minute)
	f.run()
	if len(f.delivery.submissions) != 0 {
		t.Fatal("retried before the acceptance backoff")
	}
	f.clock = f.clock.Add(domain.ReportAcceptanceRetryDelay)
	f.run()
	if len(f.delivery.submissions) != 1 {
		t.Fatalf("submissions = %d, want one after the backoff", len(f.delivery.submissions))
	}

	// A claim left by a dead daemon is requeued under the same identity.
	f.submit(f.coordA.ID, "k2", domain.SummaryWorking)
	f.clock = f.clock.Add(domain.CoordinatorSummaryBatchWindow)
	claimed, err := f.store.ClaimPendingCoordinatorSummaryBatch(ctx, "p", f.coordA.ID, f.root.ID, "dead-daemon", f.clock)
	if err != nil || len(claimed) != 1 {
		t.Fatalf("claim = %+v err=%v", claimed, err)
	}
	if n, err := f.store.RequeueClaimedCoordinatorSummaries(ctx); err != nil || n != 1 {
		t.Fatalf("requeue n=%d err=%v", n, err)
	}
	f.run()
	if len(f.delivery.submissions) != 2 || f.delivery.submissions[1].key != claimed[0].DeliveryBatchID {
		t.Fatalf("submissions = %#v, want a re-delivery under batch %q", f.delivery.submissions, claimed[0].DeliveryBatchID)
	}
}

func TestSummariesAreBatchedPerCoordinatorAndNeverMixed(t *testing.T) {
	f := newSummaryFixture(t)
	f.submit(f.coordA.ID, "a1", domain.SummaryWorking)
	f.submit(f.coordA.ID, "a2", domain.SummaryWorking)
	f.submit(f.coordB.ID, "b1", domain.SummaryWorking)

	f.run()
	if len(f.delivery.submissions) != 0 {
		t.Fatalf("delivered inside the batch window: %#v", f.delivery.submissions)
	}
	f.clock = f.clock.Add(domain.CoordinatorSummaryBatchWindow)
	f.run()
	if len(f.delivery.submissions) != 2 {
		t.Fatalf("submissions = %#v, want one turn per coordinator", f.delivery.submissions)
	}
	var both, only int
	for _, sub := range f.delivery.submissions {
		if strings.Contains(sub.message, "result a1") && strings.Contains(sub.message, "result a2") && !strings.Contains(sub.message, "result b1") {
			both++
		}
		if strings.Contains(sub.message, "result b1") && !strings.Contains(sub.message, "result a1") {
			only++
		}
	}
	if both != 1 || only != 1 {
		t.Fatalf("grouping wrong: %#v", f.delivery.submissions)
	}
}

func TestEscalationReachesRootIndependentlyOfSummaryCadence(t *testing.T) {
	f := newSummaryFixture(t)
	f.submit(f.coordA.ID, "status", domain.SummaryWorking)
	f.submit(f.coordB.ID, "quiet-status", domain.SummaryWorking)
	// An escalation lands inside the batch window of the plain summaries.
	esc := f.escalate(f.coordA.ID, "esc-1", domain.EscalationScopeDecision, f.workerA1.ID)
	if !esc.AvailableAt.Equal(esc.CreatedAt) {
		t.Fatalf("escalation available at %v, created %v, want immediate", esc.AvailableAt, esc.CreatedAt)
	}

	f.run()
	if len(f.delivery.submissions) != 1 {
		t.Fatalf("submissions = %#v, want only coordinator A's group", f.delivery.submissions)
	}
	got := f.delivery.submissions[0]
	if got.target != f.root.ID || !strings.Contains(got.message, "[escalation: scope_decision]") || !strings.Contains(got.message, "note: note esc-1") {
		t.Fatalf("escalation turn:\n%s", got.message)
	}
	if !strings.Contains(got.message, "result status") {
		t.Fatalf("escalation dropped coordinator A's earlier pending summary:\n%s", got.message)
	}
	if strings.Contains(got.message, "quiet-status") {
		t.Fatalf("escalation pulled another coordinator's group:\n%s", got.message)
	}
	if len(f.delivery.interrupts) != 1 || f.delivery.interrupts[0] != f.root.ID {
		t.Fatalf("interrupts = %v, want one root interrupt", f.delivery.interrupts)
	}
	if hist := f.history(f.coordB.ID); hist[0].DeliveryState != domain.ReportPending {
		t.Fatalf("coordinator B's plain summary delivered early: %+v", hist[0])
	}
}

func TestRepeatedEscalationsAreRateLimitedForInterruptsButNeverDropped(t *testing.T) {
	f := newSummaryFixture(t)
	f.escalate(f.coordA.ID, "esc-1", domain.EscalationWorkerFailed)
	f.run()
	f.clock = f.clock.Add(time.Second)
	f.escalate(f.coordA.ID, "esc-2", domain.EscalationOverlappingFiles)
	f.run()

	if len(f.delivery.submissions) != 2 {
		t.Fatalf("submissions = %#v, want both escalations delivered", f.delivery.submissions)
	}
	if !strings.Contains(f.delivery.submissions[1].message, "[escalation: overlapping_files]") {
		t.Fatalf("second escalation missing:\n%s", f.delivery.submissions[1].message)
	}
	if len(f.delivery.interrupts) != 1 {
		t.Fatalf("interrupts = %v, want the second one coalesced inside the window", f.delivery.interrupts)
	}
	// Another coordinator has its own window.
	f.escalate(f.coordB.ID, "esc-b", domain.EscalationWorkerFailed)
	f.run()
	if len(f.delivery.interrupts) != 2 {
		t.Fatalf("interrupts = %v, want a separate window per coordinator", f.delivery.interrupts)
	}
	// After the window a new escalation interrupts again.
	f.clock = f.clock.Add(reportsvc.ReportInterruptWindow)
	f.escalate(f.coordA.ID, "esc-3", domain.EscalationScopeDecision)
	f.run()
	if len(f.delivery.interrupts) != 3 {
		t.Fatalf("interrupts = %v, want a fresh interrupt after the window", f.delivery.interrupts)
	}
	for _, s := range append(f.history(f.coordA.ID), f.history(f.coordB.ID)...) {
		if s.DeliveryState != domain.ReportAcknowledged {
			t.Fatalf("escalation not delivered: %+v", s)
		}
	}
}

func TestBlockedSummaryWithoutReasonIsNotAnEscalation(t *testing.T) {
	f := newSummaryFixture(t)
	rec := f.submit(f.coordA.ID, "blocked-plain", domain.SummaryBlocked)
	if rec.IsEscalation() || !rec.AvailableAt.After(rec.CreatedAt) {
		t.Fatalf("blocked summary without a reason must batch like any summary: %+v", rec)
	}
	f.clock = f.clock.Add(domain.CoordinatorSummaryBatchWindow)
	f.run()
	if len(f.delivery.interrupts) != 0 || len(f.delivery.submissions) != 1 {
		t.Fatalf("interrupts=%v submissions=%d", f.delivery.interrupts, len(f.delivery.submissions))
	}
}
