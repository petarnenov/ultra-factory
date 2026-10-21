package pr

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

const (
	ciGateURL     = "https://github.com/acme/widgets/pull/42"
	ciGateHead    = "cccccccccccccccccccccccccccccccccccccccc"
	ciGateOldHead = "dddddddddddddddddddddddddddddddddddddddd"
)

type ciGateStoreFake struct {
	readyStoreFake
	allSessions []domain.SessionRecord
	runs        []domain.ReviewRun
	events      []domain.SessionCIGateEvent
	recordErr   error
}

func (f *ciGateStoreFake) ListAllSessions(context.Context) ([]domain.SessionRecord, error) {
	return f.allSessions, nil
}

func (f *ciGateStoreFake) ListReviewRunsBySession(context.Context, domain.SessionID) ([]domain.ReviewRun, error) {
	return f.runs, nil
}

func (f *ciGateStoreFake) RecordSessionCIGateEvent(_ context.Context, ev domain.SessionCIGateEvent) error {
	if f.recordErr != nil {
		return f.recordErr
	}
	ev.ID = int64(len(f.events) + 1)
	f.events = append(f.events, ev)
	return nil
}

func (f *ciGateStoreFake) ListSessionCIGateEvents(context.Context, domain.SessionID) ([]domain.SessionCIGateEvent, error) {
	return f.events, nil
}

// ciGateSetterFake records flips and applies them to the fake provider state.
type ciGateSetterFake struct {
	calls []ports.SCMDraftRequest
	err   error
	scm   *fakeSCMAction
}

func (f *ciGateSetterFake) SetPullRequestDraft(_ context.Context, r ports.SCMDraftRequest) error {
	f.calls = append(f.calls, r)
	if f.err != nil {
		return f.err
	}
	f.scm.observation.PR.Draft = r.Draft
	return nil
}

type ciGateFixture struct {
	store  *ciGateStoreFake
	scm    *fakeSCMAction
	setter *ciGateSetterFake
	svc    *ActionService
}

// newCIGateFixture returns a CI-gated session with one open PR on ciGateHead,
// starting as draft or ready as requested.
func newCIGateFixture(t *testing.T, draft bool) *ciGateFixture {
	t.Helper()
	store := &ciGateStoreFake{readyStoreFake: readyStoreFake{
		sessions: map[domain.SessionID]domain.SessionRecord{
			"worker": {ID: "worker", ProjectID: "ao", Kind: domain.KindWorker, CIGateActive: true},
		},
		prs: []domain.PullRequest{{
			URL: ciGateURL, Number: 42, Provider: "github", SessionID: "worker",
			Host: "github.com", Repo: "acme/widgets", HeadSHA: ciGateHead, Draft: draft,
		}},
	}}
	store.allSessions = []domain.SessionRecord{store.sessions["worker"]}
	scm := &fakeSCMAction{observation: ports.SCMObservation{
		Fetched: true,
		PR:      ports.SCMPRObservation{Number: 42, HeadSHA: ciGateHead, Draft: draft},
	}}
	setter := &ciGateSetterFake{scm: scm}
	return &ciGateFixture{
		store: store, scm: scm, setter: setter,
		svc: NewActionService(ActionDeps{Store: store, Reader: scm, Draft: setter}),
	}
}

func (f *ciGateFixture) addRun(verdict domain.ReviewVerdict, targetSHA string, at time.Time) {
	f.store.runs = append(f.store.runs, domain.ReviewRun{
		SessionID: "worker", PRURL: ciGateURL, TargetSHA: targetSHA,
		Status: domain.ReviewRunComplete, Verdict: verdict, CreatedAt: at,
	})
}

func TestReconcileCIGateApprovedReadiesDraftPR(t *testing.T) {
	f := newCIGateFixture(t, true)
	f.addRun(domain.VerdictApproved, ciGateHead, time.Now())

	if err := f.svc.ReconcileCIGate(context.Background(), "worker"); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if len(f.setter.calls) != 1 || f.setter.calls[0].Draft {
		t.Fatalf("flips = %+v, want one ready flip", f.setter.calls)
	}
	if len(f.store.events) != 1 || f.store.events[0].Kind != "ci_gate_ready" || f.store.events[0].HeadSHA != ciGateHead {
		t.Fatalf("events = %+v, want one ci_gate_ready for the head", f.store.events)
	}
}

func TestReconcileCIGateChangesRequestedDraftsReadyPR(t *testing.T) {
	f := newCIGateFixture(t, false)
	f.addRun(domain.VerdictChangesRequested, ciGateHead, time.Now())

	if err := f.svc.ReconcileCIGate(context.Background(), "worker"); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if len(f.setter.calls) != 1 || !f.setter.calls[0].Draft {
		t.Fatalf("flips = %+v, want one draft flip", f.setter.calls)
	}
	if len(f.store.events) != 1 || f.store.events[0].Kind != "ci_gate_draft" {
		t.Fatalf("events = %+v, want one ci_gate_draft", f.store.events)
	}
}

func TestReconcileCIGateIgnoresVerdictForStaleHead(t *testing.T) {
	f := newCIGateFixture(t, true)
	f.addRun(domain.VerdictApproved, ciGateOldHead, time.Now())

	if err := f.svc.ReconcileCIGate(context.Background(), "worker"); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if len(f.setter.calls) != 0 || len(f.store.events) != 0 {
		t.Fatalf("stale verdict flipped: calls=%+v events=%+v", f.setter.calls, f.store.events)
	}
}

func TestReconcileCIGateSkipsWhenLiveHeadMoved(t *testing.T) {
	f := newCIGateFixture(t, true)
	f.addRun(domain.VerdictApproved, ciGateHead, time.Now())
	f.scm.observation.PR.HeadSHA = ciGateOldHead

	if err := f.svc.ReconcileCIGate(context.Background(), "worker"); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if len(f.setter.calls) != 0 {
		t.Fatalf("moved head flipped: %+v", f.setter.calls)
	}
}

func TestReconcileCIGateLeavesUngatedSessionAlone(t *testing.T) {
	f := newCIGateFixture(t, true)
	rec := f.store.sessions["worker"]
	rec.CIGateActive = false
	f.store.sessions["worker"] = rec
	f.addRun(domain.VerdictApproved, ciGateHead, time.Now())

	if err := f.svc.ReconcileCIGate(context.Background(), "worker"); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	f.svc.sweepCIGatesOnce(context.Background())
	if len(f.setter.calls) != 0 || len(f.store.events) != 0 {
		t.Fatalf("ungated session touched: calls=%+v events=%+v", f.setter.calls, f.store.events)
	}
}

func TestReconcileCIGateDoesNotFlipAgainAfterHumanOverride(t *testing.T) {
	f := newCIGateFixture(t, true)
	f.addRun(domain.VerdictApproved, ciGateHead, time.Now())
	ctx := context.Background()

	if err := f.svc.ReconcileCIGate(ctx, "worker"); err != nil {
		t.Fatalf("first reconcile: %v", err)
	}
	// A person converts the PR back to draft by hand.
	f.scm.observation.PR.Draft = true
	if err := f.svc.ReconcileCIGate(ctx, "worker"); err != nil {
		t.Fatalf("second reconcile: %v", err)
	}
	if len(f.setter.calls) != 1 || len(f.store.events) != 1 {
		t.Fatalf("human override was undone: calls=%+v events=%+v", f.setter.calls, f.store.events)
	}
}

func TestReconcileCIGateRetriesFailedFlipExactlyOnce(t *testing.T) {
	f := newCIGateFixture(t, true)
	f.addRun(domain.VerdictApproved, ciGateHead, time.Now())
	ctx := context.Background()

	f.setter.err = errors.New("github 503")
	if err := f.svc.ReconcileCIGate(ctx, "worker"); err == nil {
		t.Fatal("reconcile with failing provider returned nil")
	}
	if len(f.store.events) != 0 {
		t.Fatalf("failed flip recorded an event: %+v", f.store.events)
	}
	f.setter.err = nil
	f.svc.sweepCIGatesOnce(ctx)
	f.svc.sweepCIGatesOnce(ctx)
	if len(f.setter.calls) != 2 || len(f.store.events) != 1 {
		t.Fatalf("retry: calls=%d events=%d, want 2 calls and 1 event", len(f.setter.calls), len(f.store.events))
	}
}

func TestReconcileCIGateRecordsAlreadyInStateWithoutProviderWrite(t *testing.T) {
	f := newCIGateFixture(t, false)
	f.addRun(domain.VerdictApproved, ciGateHead, time.Now())

	if err := f.svc.ReconcileCIGate(context.Background(), "worker"); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if len(f.setter.calls) != 0 {
		t.Fatalf("ready PR got a provider write: %+v", f.setter.calls)
	}
	if len(f.store.events) != 1 || f.store.events[0].Reason != "already_in_state" {
		t.Fatalf("events = %+v, want one already_in_state event", f.store.events)
	}
}

func TestReconcileCIGateConcurrentCallsFlipOnce(t *testing.T) {
	f := newCIGateFixture(t, true)
	f.addRun(domain.VerdictApproved, ciGateHead, time.Now())

	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := f.svc.ReconcileCIGate(context.Background(), "worker"); err != nil {
				t.Errorf("reconcile: %v", err)
			}
		}()
	}
	wg.Wait()
	if len(f.setter.calls) != 1 || len(f.store.events) != 1 {
		t.Fatalf("concurrent reconcile: calls=%d events=%d, want 1 each", len(f.setter.calls), len(f.store.events))
	}
}
