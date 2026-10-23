package store_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite"
)

func TestReportStorePersistsOutputsAndDeadlinesAcrossRestart(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s, err := sqlite.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	seedProject(t, s, "ao")
	sess, err := s.CreateSession(ctx, sampleRecord("ao"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	want := domain.ReportRecord{
		ID: "rpt_1", SessionID: sess.ID, ProjectID: sess.ProjectID,
		Outputs: []domain.ReportOutput{
			{Kind: domain.ReportOutputArtifact, Reference: "opaque-ref", Label: "Result"},
			{Kind: domain.ReportOutputArtifact, Reference: "second"},
			{Kind: domain.ReportOutputPRCreated, Reference: "https://github.com/o/r/pull/1"},
		},
		CreatedAt: now, DeliveryState: domain.ReportPending,
		AvailableAt: now.Add(domain.ReportBatchFallback), RepeatCount: 1,
	}
	if _, err = s.CreateReport(ctx, want); err != nil {
		t.Fatal(err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = sqlite.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	got, ok, err := s.GetReport(ctx, want.ID)
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if got.SessionID != sess.ID || got.ProjectID != sess.ProjectID || len(got.Outputs) != 3 || got.Outputs[0].Label != "Result" || got.Outputs[1].Reference != "second" || got.Outputs[2].Kind != domain.ReportOutputPRCreated || !got.AvailableAt.Equal(want.AvailableAt) {
		t.Fatalf("got=%+v", got)
	}
	if pending, err := s.ListPendingReports(ctx, now, 10); err != nil || len(pending) != 0 {
		t.Fatalf("early pending=%+v err=%v", pending, err)
	}
	urls, err := s.ListReportedPRURLs(ctx, sess.ID)
	if err != nil || len(urls) != 1 || urls[0] != "https://github.com/o/r/pull/1" {
		t.Fatalf("reported PR URLs=%v err=%v", urls, err)
	}
	events, err := s.EventsAfter(ctx, 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	var reportEvents int
	for _, event := range events {
		if event.SessionID == string(sess.ID) && event.Type == "session_updated" {
			reportEvents++
		}
	}
	if reportEvents != 1 {
		t.Fatalf("reported PR CDC events=%d, want 1", reportEvents)
	}
	pending, err := s.ListPendingReports(ctx, want.AvailableAt, 10)
	if err != nil || len(pending) != 1 || pending[0].ID != want.ID || len(pending[0].Outputs) != 3 {
		t.Fatalf("pending=%+v err=%v", pending, err)
	}
}

func TestTerminalReportOutcomePersistsAtomicallyAndIsMonotonicAcrossRestart(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s, err := sqlite.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	seedProject(t, s, "ao")
	sess, err := s.CreateSession(ctx, sampleRecord("ao"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	base := domain.ReportRecord{
		SessionID: sess.ID, ProjectID: sess.ProjectID, State: domain.ReportDone,
		Note: "final result", CreatedAt: now, DeliveryState: domain.ReportPending,
		AvailableAt: now.Add(domain.ReportSettlementWindow), SettlementDeadline: now.Add(domain.ReportSettlementWindow), RepeatCount: 1,
		Outputs: []domain.ReportOutput{{Kind: domain.ReportOutputArtifact, Reference: "opaque://final"}},
	}
	first := base
	first.ID = "rpt_final"
	first.TerminalOutcome = domain.ReportOutcomeSuccess
	created, err := s.CreateReport(ctx, first)
	if err != nil {
		t.Fatal(err)
	}
	if created.ID != first.ID {
		t.Fatalf("first final report ID=%s, want %s", created.ID, first.ID)
	}
	retry := first
	retry.ID = "rpt_retry_final"
	retry.Note = "retried request"
	created, err = s.CreateReport(ctx, retry)
	if err != nil || created.ID != first.ID {
		t.Fatalf("same-outcome retry report=%+v err=%v, want original %s", created, err, first.ID)
	}
	interim := base
	interim.ID = "rpt_late_checkpoint"
	interim.State = domain.ReportCheckpoint
	interim.Note = "late informational report"
	interim.AvailableAt = now.Add(domain.ReportBatchFallback)
	interim.SettlementDeadline = time.Time{}
	interim.Outputs = nil
	if _, err := s.CreateReport(ctx, interim); err != nil {
		t.Fatal(err)
	}
	second := base
	second.ID = "rpt_duplicate_final"
	second.TerminalOutcome = domain.ReportOutcomeFailure
	if _, err := s.CreateReport(ctx, second); !errors.Is(err, domain.ErrTerminalReportConflict) {
		t.Fatalf("conflicting terminal report err=%v, want conflict", err)
	}
	outcome, found, err := s.GetTerminalReportOutcome(ctx, sess.ID)
	if err != nil || !found || outcome != domain.ReportOutcomeSuccess {
		t.Fatalf("terminal outcome=%q found=%v err=%v", outcome, found, err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = sqlite.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	outcome, found, err = s.GetTerminalReportOutcome(ctx, sess.ID)
	if err != nil || !found || outcome != domain.ReportOutcomeSuccess {
		t.Fatalf("restarted outcome=%q found=%v err=%v", outcome, found, err)
	}
	got, ok, err := s.GetReport(ctx, first.ID)
	if err != nil || !ok || got.TerminalOutcome != domain.ReportOutcomeSuccess || len(got.Outputs) != 1 {
		t.Fatalf("durable report=%+v ok=%v err=%v", got, ok, err)
	}
	all, err := s.ListReportsBySession(ctx, sess.ID)
	if err != nil || len(all) != 2 {
		t.Fatalf("reports=%+v err=%v", all, err)
	}
}

func TestTerminalReportDeliveryRemainsPendingAfterWorkerTermination(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	seedProject(t, s, "ao")
	now := time.Now().UTC().Truncate(time.Second)
	worker, err := s.CreateSession(ctx, domain.SessionRecord{ProjectID: "ao", Kind: domain.KindWorker,
		Harness: domain.HarnessClaudeCode, Activity: domain.Activity{State: domain.ActivityActive}, CreatedAt: now, UpdatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	report, err := s.CreateReport(ctx, domain.ReportRecord{ID: "final-report", SessionID: worker.ID, ProjectID: "ao",
		State: domain.ReportDone, TerminalOutcome: domain.ReportOutcomeFailure, Note: "terminal failure",
		CreatedAt: now, AvailableAt: now, SettlementDeadline: now.Add(5 * time.Minute),
		DeliveryState: domain.ReportPending, RepeatCount: 1})
	if err != nil {
		t.Fatal(err)
	}
	worker.IsTerminated = true
	worker.UpdatedAt = now.Add(time.Second)
	if err := s.UpdateSession(ctx, worker); err != nil {
		t.Fatal(err)
	}
	got, err := s.ListPendingReports(ctx, report.SettlementDeadline, 10)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, pending := range got {
		if pending.ID == report.ID && pending.TerminalOutcome == domain.ReportOutcomeFailure {
			found = true
		}
	}
	if !found {
		t.Fatalf("terminal report was not pending after termination: %+v", got)
	}
}

func TestReportStoreRejectsInvalidRecord(t *testing.T) {
	s := newTestStore(t)
	_, err := s.CreateReport(context.Background(), domain.ReportRecord{})
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func TestReportStoreClaimAcknowledgeAndTokenFence(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	seedProject(t, s, "ao")
	sess, err := s.CreateSession(ctx, sampleRecord("ao"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	rec := domain.ReportRecord{ID: "rpt_lease", SessionID: sess.ID, ProjectID: sess.ProjectID, State: domain.ReportDone, Note: "done", CreatedAt: now.Add(-domain.ReportSettlementWindow), DeliveryState: domain.ReportPending, AvailableAt: now, SettlementDeadline: now, RepeatCount: 1}
	if _, err = s.CreateReport(ctx, rec); err != nil {
		t.Fatal(err)
	}
	claimed, ok, err := s.ClaimReport(ctx, rec.ID, "lease-1", now)
	if err != nil || !ok || claimed.DeliveryAttempts != 1 || claimed.DeliveryState != domain.ReportClaimed {
		t.Fatalf("claimed=%+v ok=%v err=%v", claimed, ok, err)
	}
	if _, ok, err = s.ClaimReport(ctx, rec.ID, "lease-2", now); err != nil || ok {
		t.Fatalf("second claim ok=%v err=%v", ok, err)
	}
	if _, ok, err = s.AcknowledgeReport(ctx, rec.ID, "wrong", now.Add(time.Second)); err != nil || ok {
		t.Fatalf("wrong ack ok=%v err=%v", ok, err)
	}
	acked, ok, err := s.AcknowledgeReport(ctx, rec.ID, "lease-1", now.Add(time.Second))
	if err != nil || !ok || acked.DeliveryState != domain.ReportAcknowledged {
		t.Fatalf("acked=%+v ok=%v err=%v", acked, ok, err)
	}
}

func TestReportStoreRequeuesInterruptedClaimOnRestartAndFencesOldToken(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s, err := sqlite.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	seedProject(t, s, "ao")
	sess, err := s.CreateSession(ctx, sampleRecord("ao"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	rec := domain.ReportRecord{ID: "rpt_restart", SessionID: sess.ID, ProjectID: sess.ProjectID, Message: "information", CreatedAt: now.Add(-time.Hour), DeliveryState: domain.ReportPending, AvailableAt: now, RepeatCount: 1}
	if _, err = s.CreateReport(ctx, rec); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := s.ClaimReport(ctx, rec.ID, "old-token", now); err != nil || !ok {
		t.Fatalf("claim ok=%v err=%v", ok, err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	s, err = sqlite.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if count, err := s.RequeueClaimedReports(ctx); err != nil || count != 1 {
		t.Fatalf("restart recovery count=%d err=%v", count, err)
	}
	recovered, ok, err := s.GetReport(ctx, rec.ID)
	if err != nil || !ok || recovered.DeliveryState != domain.ReportPending || recovered.ClaimToken != "" || !recovered.ClaimedAt.IsZero() || recovered.DeliveryAttempts != 1 || !strings.Contains(recovered.LastError, "restart") {
		t.Fatalf("recovered=%+v ok=%v err=%v", recovered, ok, err)
	}
	if _, ok, err := s.AcknowledgeReport(ctx, rec.ID, "old-token", now.Add(time.Second)); err != nil || ok {
		t.Fatalf("stale acknowledge ok=%v err=%v", ok, err)
	}
	claimed, ok, err := s.ClaimReport(ctx, rec.ID, "new-token", now.Add(2*time.Second))
	if err != nil || !ok || claimed.DeliveryAttempts != 2 {
		t.Fatalf("reclaim=%+v ok=%v err=%v", claimed, ok, err)
	}
	if _, ok, err := s.AcknowledgeReport(ctx, rec.ID, "old-token", now.Add(3*time.Second)); err != nil || ok {
		t.Fatalf("old token crossed fence ok=%v err=%v", ok, err)
	}
	if _, ok, err := s.AcknowledgeReport(ctx, rec.ID, "new-token", now.Add(4*time.Second)); err != nil || !ok {
		t.Fatalf("new acknowledge ok=%v err=%v", ok, err)
	}
}

func TestReportStoreDurableBatchIdentityExcludesLaterReportsOnRetry(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	seedProject(t, s, "ao")
	sess, err := s.CreateSession(ctx, sampleRecord("ao"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	create := func(id string) {
		t.Helper()
		_, err := s.CreateReport(ctx, domain.ReportRecord{ID: id, SessionID: sess.ID, ProjectID: "ao", Message: id, CreatedAt: now, DeliveryState: domain.ReportPending, AvailableAt: now, RepeatCount: 1})
		if err != nil {
			t.Fatal(err)
		}
	}
	create("first")
	create("second")
	claimed, err := s.ClaimPendingReportBatch(ctx, "ao", "root", "root", "token-1", now)
	if err != nil || len(claimed) != 2 || claimed[0].DeliveryBatchID == "" || claimed[0].DeliveryBatchID != claimed[1].DeliveryBatchID {
		t.Fatalf("claimed = %+v, err = %v", claimed, err)
	}
	batchID := claimed[0].DeliveryBatchID
	if _, err := s.ReleaseReportBatch(ctx, "ao", "token-1", now, "accepted response lost"); err != nil {
		t.Fatal(err)
	}
	create("later")
	retried, err := s.ClaimPendingReportBatch(ctx, "ao", "root", "root", "token-2", now)
	if err != nil || len(retried) != 2 || retried[0].DeliveryBatchID != batchID {
		t.Fatalf("retried = %+v, err = %v", retried, err)
	}
	if _, err := s.AcknowledgeReportBatch(ctx, "ao", "token-2", now); err != nil {
		t.Fatal(err)
	}
	next, err := s.ClaimPendingReportBatch(ctx, "ao", "root", "root", "token-3", now)
	if err != nil || len(next) != 1 || next[0].ID != "later" || next[0].DeliveryBatchID == batchID {
		t.Fatalf("next = %+v, err = %v", next, err)
	}
}

func TestReportStoreClaimsOnlyTheRecipientsOwnerGroup(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	seedProject(t, s, "ao")
	now := time.Now().UTC().Truncate(time.Second)

	coordinator := sampleRecord("ao")
	coordinator.Kind = domain.KindCoordinator
	coordinator.CoordinatorScope = "auth"
	coordinator, err := s.CreateSession(ctx, coordinator)
	if err != nil {
		t.Fatal(err)
	}
	owned := sampleRecord("ao")
	owned.OwnerSessionID = &coordinator.ID
	owned, err = s.CreateSession(ctx, owned)
	if err != nil {
		t.Fatal(err)
	}
	rootOwned, err := s.CreateSession(ctx, sampleRecord("ao"))
	if err != nil {
		t.Fatal(err)
	}
	for _, rec := range []domain.ReportRecord{
		{ID: "rpt_owned", SessionID: owned.ID, ProjectID: "ao", Message: "owned", CreatedAt: now, DeliveryState: domain.ReportPending, AvailableAt: now, RepeatCount: 1},
		{ID: "rpt_root", SessionID: rootOwned.ID, ProjectID: "ao", Message: "root", CreatedAt: now.Add(time.Second), DeliveryState: domain.ReportPending, AvailableAt: now.Add(time.Second), RepeatCount: 1},
	} {
		if _, err := s.CreateReport(ctx, rec); err != nil {
			t.Fatal(err)
		}
	}

	ownedBatch, err := s.ClaimPendingReportBatch(ctx, "ao", coordinator.ID, "root-session", "owned-token", now)
	if err != nil {
		t.Fatal(err)
	}
	if len(ownedBatch) != 1 || ownedBatch[0].ID != "rpt_owned" || ownedBatch[0].DeliveryOwnerID != string(coordinator.ID) {
		t.Fatalf("owned batch = %+v", ownedBatch)
	}
	if _, err := s.AcknowledgeReportBatch(ctx, "ao", "owned-token", now); err != nil {
		t.Fatal(err)
	}

	rootBatch, err := s.ClaimPendingReportBatch(ctx, "ao", "root-session", "root-session", "root-token", now)
	if err != nil {
		t.Fatal(err)
	}
	if len(rootBatch) != 1 || rootBatch[0].ID != "rpt_root" || rootBatch[0].DeliveryOwnerID != "root-session" {
		t.Fatalf("root batch = %+v", rootBatch)
	}
}

func TestReportStoreRootClaimsAnExitedOwnersWorkersWithoutClearingOwnership(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	seedProject(t, s, "ao")
	now := time.Now().UTC().Truncate(time.Second)

	coordinator := sampleRecord("ao")
	coordinator.Kind = domain.KindCoordinator
	coordinator.CoordinatorScope = "auth"
	coordinator, err := s.CreateSession(ctx, coordinator)
	if err != nil {
		t.Fatal(err)
	}
	owned := sampleRecord("ao")
	owned.OwnerSessionID = &coordinator.ID
	owned, err = s.CreateSession(ctx, owned)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateReport(ctx, domain.ReportRecord{ID: "rpt_owned", SessionID: owned.ID, ProjectID: "ao", Message: "owned", CreatedAt: now, DeliveryState: domain.ReportPending, AvailableAt: now, RepeatCount: 1}); err != nil {
		t.Fatal(err)
	}

	none, err := s.ClaimPendingReportBatch(ctx, "ao", "root-session", "root-session", "plain-token", now)
	if err != nil || len(none) != 0 {
		t.Fatalf("plain root claim = %+v, err = %v", none, err)
	}
	batch, err := s.ClaimPendingReportBatch(ctx, "ao", "root-session", "root-session", "exited-token", now, coordinator.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(batch) != 1 || batch[0].ID != "rpt_owned" || batch[0].DeliveryOwnerID != "root-session" {
		t.Fatalf("batch = %+v", batch)
	}
	got, _, err := s.GetSession(ctx, owned.ID)
	if err != nil || got.OwnerSessionID == nil || *got.OwnerSessionID != coordinator.ID {
		t.Fatalf("ownership changed: %+v err = %v", got.OwnerSessionID, err)
	}
}

func TestReportStoreKeepsAnExitedOwnerBatchApartFromRootReports(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	seedProject(t, s, "ao")
	now := time.Now().UTC().Truncate(time.Second)

	coordinator := sampleRecord("ao")
	coordinator.Kind = domain.KindCoordinator
	coordinator.CoordinatorScope = "auth"
	coordinator, err := s.CreateSession(ctx, coordinator)
	if err != nil {
		t.Fatal(err)
	}
	owned := sampleRecord("ao")
	owned.OwnerSessionID = &coordinator.ID
	owned, err = s.CreateSession(ctx, owned)
	if err != nil {
		t.Fatal(err)
	}
	rootOwned, err := s.CreateSession(ctx, sampleRecord("ao"))
	if err != nil {
		t.Fatal(err)
	}
	// The root report is older but settles later, so it is still unbatched
	// when the exited owner's group runs first.
	for _, rec := range []domain.ReportRecord{
		{ID: "rpt_root", SessionID: rootOwned.ID, ProjectID: "ao", Message: "root", CreatedAt: now, DeliveryState: domain.ReportPending, AvailableAt: now.Add(time.Minute), RepeatCount: 1},
		{ID: "rpt_exited", SessionID: owned.ID, ProjectID: "ao", Message: "stuck", CreatedAt: now.Add(time.Second), DeliveryState: domain.ReportPending, AvailableAt: now, RepeatCount: 1},
	} {
		if _, err := s.CreateReport(ctx, rec); err != nil {
			t.Fatal(err)
		}
	}
	exited, err := s.ClaimPendingReportBatch(ctx, "ao", "root-session", "root-session", "exited-token", now, coordinator.ID)
	if err != nil || len(exited) != 1 || exited[0].ID != "rpt_exited" {
		t.Fatalf("exited group = %+v, err = %v", exited, err)
	}
	if _, err := s.DeferReportBatch(ctx, "ao", "exited-token", now.Add(time.Minute), "stuck"); err != nil {
		t.Fatal(err)
	}
	root, err := s.ClaimPendingReportBatch(ctx, "ao", "root-session", "root-session", "root-token", now.Add(time.Minute))
	if err != nil || len(root) != 1 || root[0].ID != "rpt_root" {
		t.Fatalf("root group = %+v, err = %v", root, err)
	}
	if root[0].DeliveryBatchID == exited[0].DeliveryBatchID {
		t.Fatalf("root and exited-owner reports share batch %s", root[0].DeliveryBatchID)
	}
}

func TestReportStoreInterruptLimitSurvivesRestart(t *testing.T) {

	ctx := context.Background()
	dir := t.TempDir()
	s, err := sqlite.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	seedProject(t, s, "ao")
	sess, err := s.CreateSession(ctx, sampleRecord("ao"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	if acquired, _, err := s.AcquireReportInterrupt(ctx, sess.ID, now, 3*time.Minute); err != nil || !acquired {
		t.Fatalf("first acquired = %v, err = %v", acquired, err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = sqlite.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if acquired, next, err := s.AcquireReportInterrupt(ctx, sess.ID, now.Add(2*time.Minute), 3*time.Minute); err != nil || acquired || !next.Equal(now.Add(3*time.Minute)) {
		t.Fatalf("restart acquired = %v, err = %v", acquired, err)
	}
	if acquired, _, err := s.AcquireReportInterrupt(ctx, sess.ID, now.Add(3*time.Minute), 3*time.Minute); err != nil || !acquired {
		t.Fatalf("boundary acquired = %v, err = %v", acquired, err)
	}
}

func TestReportStoreReleaseReportInterruptRestoresTheWindow(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	seedProject(t, s, "ao")
	sess, err := s.CreateSession(ctx, sampleRecord("ao"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	if acquired, _, err := s.AcquireReportInterrupt(ctx, sess.ID, now, 3*time.Minute); err != nil || !acquired {
		t.Fatalf("acquire = %v, err = %v", acquired, err)
	}
	if acquired, _, err := s.AcquireReportInterrupt(ctx, sess.ID, now.Add(time.Minute), 3*time.Minute); err != nil || acquired {
		t.Fatalf("occupied acquire = %v, err = %v", acquired, err)
	}
	// A release for another acquisition time must leave the held slot alone.
	if err := s.ReleaseReportInterrupt(ctx, sess.ID, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if acquired, _, err := s.AcquireReportInterrupt(ctx, sess.ID, now.Add(time.Minute), 3*time.Minute); err != nil || acquired {
		t.Fatalf("stale release freed the slot: acquired = %v, err = %v", acquired, err)
	}
	if err := s.ReleaseReportInterrupt(ctx, sess.ID, now); err != nil {
		t.Fatal(err)
	}
	if acquired, _, err := s.AcquireReportInterrupt(ctx, sess.ID, now.Add(time.Minute), 3*time.Minute); err != nil || !acquired {
		t.Fatalf("released slot not reacquired: acquired = %v, err = %v", acquired, err)
	}
}

func TestListReportsByProjectPageCursorAcrossHostsAndTies(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	seedProject(t, s, "ao")
	sess, err := s.CreateSession(ctx, sampleRecord("ao"))
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 10, 18, 10, 0, 0, 0, time.UTC)
	localReports := []string{"local-alpha", "local-beta"}
	for _, id := range localReports {
		if _, err := s.CreateReport(ctx, domain.ReportRecord{
			ID: id, SessionID: sess.ID, ProjectID: "ao", State: domain.ReportDone,
			Note: id, CreatedAt: at, AvailableAt: at, SettlementDeadline: at.Add(time.Hour),
			DeliveryState: domain.ReportPending, RepeatCount: 1,
		}); err != nil {
			t.Fatalf("create local %s: %v", id, err)
		}
	}
	if err := s.RecordRemoteReportHistory(ctx, "host-z", "worker-z", []domain.ReportRecord{{
		ID: "remote-z", SessionID: "worker-z", ProjectID: "ao", SourceProjectID: "ao",
		State: domain.ReportDone, Note: "remote z", HostLabel: "Build box", Agent: "codex",
		SessionDisplayName: "worker z", CreatedAt: at, RepeatCount: 1,
	}}); err != nil {
		t.Fatalf("record remote z: %v", err)
	}
	if err := s.RecordRemoteReportHistory(ctx, "host-alpha", "worker-a", []domain.ReportRecord{{
		ID: "remote-a", SessionID: "worker-a", ProjectID: "ao", SourceProjectID: "ao",
		State: domain.ReportDone, Note: "remote a", HostLabel: "Test box", Agent: "claude-code",
		SessionDisplayName: "worker a", CreatedAt: at, RepeatCount: 1,
	}}); err != nil {
		t.Fatalf("record remote a: %v", err)
	}

	first, err := s.ListReportsByProjectPage(ctx, "ao", time.Time{}, "", "", "", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) < 2 || first[0].HostID != "host-z" || first[1].HostID != "host-alpha" {
		t.Fatalf("first page = %+v", first)
	}
	first = first[:2]
	second, err := s.ListReportsByProjectPage(ctx, "ao", first[1].CreatedAt, first[1].HostID, string(first[1].SessionID), first[1].ID, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(second) != 2 {
		t.Fatalf("second page = %+v", second)
	}
	third, err := s.ListReportsByProjectPage(ctx, "ao", second[1].CreatedAt, second[1].HostID, string(second[1].SessionID), second[1].ID, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(third) != 0 {
		t.Fatalf("third page = %+v, want none", third)
	}
	seen := map[string]bool{}
	for _, page := range [][]domain.ReportRecord{first, second} {
		for _, report := range page {
			key := report.HostID + "/" + string(report.SessionID) + "/" + report.ID
			if seen[key] {
				t.Fatalf("duplicate report %s", key)
			}
			seen[key] = true
		}
	}
	if len(seen) != 4 {
		t.Fatalf("got %d distinct reports, want 4", len(seen))
	}
}
