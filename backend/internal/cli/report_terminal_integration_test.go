package cli

import (
	"context"
	"errors"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/controllers"
	"github.com/aoagents/agent-orchestrator/backend/internal/lifecycle"
	"github.com/aoagents/agent-orchestrator/backend/internal/observe/reaper"
	reportsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/report"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/sqlitetest"
)

type reportTestTerminator struct {
	store *sqlite.Store
	calls int
	fail  bool
}

func (k *reportTestTerminator) Kill(ctx context.Context, id domain.SessionID) (bool, error) {
	k.calls++
	if k.fail {
		k.fail = false
		return false, errors.New("transient termination failure")
	}
	rec, ok, err := k.store.GetSession(ctx, id)
	if err != nil || !ok {
		return false, err
	}
	rec.IsTerminated = true
	rec.Activity.State = domain.ActivityExited
	rec.UpdatedAt = time.Now().UTC()
	return true, k.store.UpdateSession(ctx, rec)
}

type reportTestArtifacts struct {
	seen []string
}

func (a *reportTestArtifacts) RecordReportedArtifact(_ context.Context, _ domain.SessionID, ref string) {
	a.seen = append(a.seen, ref)
}

type reportTestDelivery struct{ submitted []string }

func (d *reportTestDelivery) Submit(_ context.Context, _ domain.SessionID, message, _ string) error {
	d.submitted = append(d.submitted, message)
	return nil
}

func (*reportTestDelivery) Interrupt(context.Context, domain.SessionID) error { return nil }

func TestReportFinalFlagsPersistBeforeTerminationAndOrdinaryStatesStayLive(t *testing.T) {
	ctx := context.Background()
	storeDir := t.TempDir()
	store, err := sqlitetest.Open(storeDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	now := time.Now().UTC()
	if err := store.UpsertProject(ctx, domain.ProjectRecord{ID: "ao", Path: t.TempDir(), RegisteredAt: now}); err != nil {
		t.Fatal(err)
	}
	_, err = store.CreateSession(ctx, domain.SessionRecord{ProjectID: "ao", Kind: domain.KindOrchestrator,
		Mode: domain.SessionModeChat, Harness: domain.HarnessClaudeCode,
		Activity: domain.Activity{State: domain.ActivityIdle}, CreatedAt: now, UpdatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	worker, err := store.CreateSession(ctx, domain.SessionRecord{ProjectID: "ao", Kind: domain.KindWorker,
		Harness: domain.HarnessClaudeCode, Metadata: domain.SessionMetadata{
			Branch: "feat/research", WorkspacePath: filepath.Join(storeDir, "workspace"),
			WorkspaceRepoPath: filepath.Join(storeDir, "repo"),
		}, Activity: domain.Activity{State: domain.ActivityActive}, CreatedAt: now, UpdatedAt: now})
	if err != nil {
		t.Fatal(err)
	}

	lcm := lifecycle.New(store, nil)
	terminator := &reportTestTerminator{store: store}
	lcm.SetCompletionTerminator(terminator)
	delivery := &reportTestDelivery{}
	coordinator := reportsvc.NewCoordinator(reportsvc.CoordinatorDeps{
		Store: store, Delivery: delivery,
		Now: func() time.Time { return time.Now().UTC().Add(domain.ReportSettlementWindow) },
	})
	artifacts := &reportTestArtifacts{}
	var terminationErrors []error
	reports := reportsvc.New(reportsvc.Deps{Store: store, Artifacts: artifacts, OnCreated: func(rec domain.ReportRecord) {
		coordinator.Wake()
		if rec.TerminalOutcome != "" {
			if err := lcm.ApplyTerminalReport(ctx, rec.SessionID); err != nil {
				terminationErrors = append(terminationErrors, err)
			}
		}
	}})
	router := chi.NewRouter()
	router.Route("/api/v1", func(api chi.Router) { (&controllers.ReportsController{Svc: reports}).Register(api) })
	server := httptest.NewServer(router)
	t.Cleanup(server.Close)
	cfg := setConfigEnv(t)
	writeRunFileFor(t, cfg, server)
	t.Setenv("AO_SESSION_ID", string(worker.ID))

	for _, tc := range []struct {
		name string
		args []string
	}{
		{name: "checkpoint", args: []string{"report", "--checkpoint", "--note", "progress"}},
		{name: "needs input", args: []string{"report", "--needs-input", "--note", "question"}},
		{name: "stuck remains recoverable", args: []string{"report", "--stuck", "--note", "blocked"}},
		{name: "ordinary done remains recoverable", args: []string{"report", "--done", "--note", "milestone"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, _, err := executeCLI(t, Deps{ProcessAlive: func(int) bool { return true }}, tc.args...); err != nil {
				t.Fatal(err)
			}
		})
	}
	if rec, _, err := store.GetSession(ctx, worker.ID); err != nil || rec.IsTerminated {
		t.Fatalf("recoverable reports terminated worker: %+v err=%v", rec, err)
	}
	if _, found, err := store.GetTerminalReportOutcome(ctx, worker.ID); err != nil || found {
		t.Fatalf("recoverable reports created terminal fact: found=%v err=%v", found, err)
	}

	if _, _, err := executeCLI(t, Deps{ProcessAlive: func(int) bool { return true }},
		"report", "--final-success", "--note", "research complete", "--artifact", "opaque://result"); err != nil {
		t.Fatal(err)
	}
	rec, _, err := store.GetSession(ctx, worker.ID)
	if err != nil || !rec.IsTerminated || terminator.calls != 1 {
		t.Fatalf("final report did not terminate once: session=%+v calls=%d err=%v", rec, terminator.calls, err)
	}
	if rec.Metadata.Branch != "feat/research" || rec.Metadata.WorkspacePath != filepath.Join(storeDir, "workspace") ||
		rec.Metadata.WorkspaceRepoPath != filepath.Join(storeDir, "repo") {
		t.Fatalf("termination discarded the worker workspace metadata: %+v", rec.Metadata)
	}
	outcome, found, err := store.GetTerminalReportOutcome(ctx, worker.ID)
	if err != nil || !found || outcome != domain.ReportOutcomeSuccess {
		t.Fatalf("durable outcome=%q found=%v err=%v", outcome, found, err)
	}
	if len(terminationErrors) != 0 {
		t.Fatalf("successful final report termination errors = %v", terminationErrors)
	}
	reportsForWorker, err := store.ListReportsBySession(ctx, worker.ID)
	if err != nil || len(reportsForWorker) != 6 {
		t.Fatalf("reports=%d err=%v, want four recoverable reports, one final report, and one termination checkpoint", len(reportsForWorker), err)
	}
	var last domain.ReportRecord
	for _, report := range reportsForWorker {
		if report.TerminalOutcome == domain.ReportOutcomeSuccess {
			last = report
		}
	}
	if last.ID == "" {
		t.Fatal("final report was not found in durable history")
	}
	if last.State != domain.ReportDone || last.TerminalOutcome != domain.ReportOutcomeSuccess || len(last.Outputs) != 1 || last.Outputs[0].Reference != "opaque://result" {
		t.Fatalf("final report was not fully durable before termination: %+v", last)
	}
	if len(artifacts.seen) != 1 || artifacts.seen[0] != "opaque://result" {
		t.Fatalf("artifact recorder ran before termination: %v", artifacts.seen)
	}
	if _, err := coordinator.RunDue(ctx); err != nil {
		t.Fatal(err)
	}
	if len(delivery.submitted) != 1 || !strings.Contains(delivery.submitted[0], "[done/success]") ||
		!strings.Contains(delivery.submitted[0], "opaque://result") {
		t.Fatalf("final report was not delivered after termination: %v", delivery.submitted)
	}
	delivered, _, err := store.GetReport(ctx, last.ID)
	if err != nil || delivered.DeliveryState != domain.ReportAcknowledged {
		t.Fatalf("final report delivery was not acknowledged: %+v err=%v", delivered, err)
	}

	if _, _, err := executeCLI(t, Deps{ProcessAlive: func(int) bool { return true }},
		"report", "--final-success", "--note", "duplicate request"); err != nil {
		t.Fatalf("duplicate final request was not idempotent: %v", err)
	}
	afterDuplicate, err := store.ListReportsBySession(ctx, worker.ID)
	if err != nil || len(afterDuplicate) != len(reportsForWorker) || terminator.calls != 1 || len(artifacts.seen) != 1 {
		t.Fatalf("duplicate request added report or repeated effects: reports=%d termination=%d artifacts=%v err=%v", len(afterDuplicate), terminator.calls, artifacts.seen, err)
	}
	if _, _, err := executeCLI(t, Deps{ProcessAlive: func(int) bool { return true }},
		"report", "--final-failure", "--note", "conflicting duplicate"); err == nil {
		t.Fatal("conflicting final outcome unexpectedly succeeded")
	}

	failureWorker, err := store.CreateSession(ctx, domain.SessionRecord{ProjectID: "ao", Kind: domain.KindWorker,
		Harness: domain.HarnessClaudeCode, Activity: domain.Activity{State: domain.ActivityActive}, CreatedAt: now, UpdatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("AO_SESSION_ID", string(failureWorker.ID))
	terminator.fail = true
	if _, _, err := executeCLI(t, Deps{ProcessAlive: func(int) bool { return true }},
		"report", "--final-failure", "--note", "required source unavailable"); err != nil {
		t.Fatal(err)
	}
	failure, _, err := store.GetSession(ctx, failureWorker.ID)
	if err != nil || failure.IsTerminated || terminator.calls != 2 || len(terminationErrors) != 1 {
		t.Fatalf("transient failure state: session=%+v calls=%d errors=%v err=%v", failure, terminator.calls, terminationErrors, err)
	}
	outcome, found, err = store.GetTerminalReportOutcome(ctx, failureWorker.ID)
	if err != nil || !found || outcome != domain.ReportOutcomeFailure {
		t.Fatalf("durable failure outcome=%q found=%v err=%v", outcome, found, err)
	}

	// Reopen the same SQLite database as a restarted daemon. Reconciliation
	// retries from the committed terminal fact without a certain runtime probe.
	server.Close()
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = sqlite.Open(storeDir)
	if err != nil {
		t.Fatal(err)
	}
	manager := lifecycle.New(store, nil)
	restartedTerminator := &reportTestTerminator{store: store}
	manager.SetCompletionTerminator(restartedTerminator)
	if err := reaper.New(manager, store, nil, reaper.Config{}).Tick(ctx); err != nil {
		t.Fatal(err)
	}
	failure, _, err = store.GetSession(ctx, failureWorker.ID)
	if err != nil || !failure.IsTerminated || restartedTerminator.calls != 1 {
		t.Fatalf("restart reconciliation failed: session=%+v calls=%d err=%v", failure, restartedTerminator.calls, err)
	}
	failureDelivery := &reportTestDelivery{}
	failureCoordinator := reportsvc.NewCoordinator(reportsvc.CoordinatorDeps{
		Store: store, Delivery: failureDelivery,
		Now: func() time.Time { return time.Now().UTC().Add(domain.ReportSettlementWindow) },
	})
	if _, err := failureCoordinator.RunDue(ctx); err != nil {
		t.Fatal(err)
	}
	if len(failureDelivery.submitted) != 1 || !strings.Contains(failureDelivery.submitted[0], "[done/failure]") {
		t.Fatalf("durable terminal failure was not delivered after restart: %v", failureDelivery.submitted)
	}
	failureReports, err := store.ListReportsBySession(ctx, failureWorker.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, report := range failureReports {
		if report.TerminalOutcome == domain.ReportOutcomeFailure {
			if report.DeliveryState != domain.ReportAcknowledged {
				t.Fatalf("terminal failure delivery state=%s, want acknowledged", report.DeliveryState)
			}
			return
		}
	}
	t.Fatal("durable terminal failure report missing after restart")
}
