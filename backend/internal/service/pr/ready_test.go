package pr

import (
	"context"
	"errors"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/apierr"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

type readyStoreFake struct {
	sessions map[domain.SessionID]domain.SessionRecord
	prs      []domain.PullRequest
	events   []domain.SessionPRReadyEvent
}

func (f *readyStoreFake) GetPR(context.Context, string) (domain.PullRequest, bool, error) {
	return domain.PullRequest{}, false, nil
}

func (f *readyStoreFake) GetSession(_ context.Context, id domain.SessionID) (domain.SessionRecord, bool, error) {
	rec, ok := f.sessions[id]
	return rec, ok, nil
}

func (f *readyStoreFake) GetProject(context.Context, string) (domain.ProjectRecord, bool, error) {
	return domain.ProjectRecord{}, false, nil
}

func (f *readyStoreFake) ListPRsBySession(context.Context, domain.SessionID) ([]domain.PullRequest, error) {
	return f.prs, nil
}

func (f *readyStoreFake) RecordSessionPRReadyEvent(_ context.Context, ev domain.SessionPRReadyEvent) error {
	f.events = append(f.events, ev)
	return nil
}

type draftSetterFake struct {
	requests []ports.SCMDraftRequest
	err      error
}

func (f *draftSetterFake) SetPullRequestDraft(_ context.Context, r ports.SCMDraftRequest) error {
	f.requests = append(f.requests, r)
	return f.err
}

const readyHead = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

func readyFixture(draft bool) (*readyStoreFake, *fakeSCMAction, *draftSetterFake, *ActionService) {
	owner := domain.SessionID("coord")
	store := &readyStoreFake{
		sessions: map[domain.SessionID]domain.SessionRecord{
			"root":   {ID: "root", ProjectID: "ao", Kind: domain.KindOrchestrator},
			"coord":  {ID: "coord", ProjectID: "ao", Kind: domain.KindCoordinator},
			"other":  {ID: "other", ProjectID: "ao", Kind: domain.KindCoordinator},
			"worker": {ID: "worker", ProjectID: "ao", Kind: domain.KindWorker, OwnerSessionID: &owner},
			"dead":   {ID: "dead", ProjectID: "ao", Kind: domain.KindOrchestrator, IsTerminated: true},
		},
		prs: []domain.PullRequest{{
			URL: "https://github.com/acme/widgets/pull/42", Number: 42, Provider: "github",
			Host: "github.com", Repo: "acme/widgets", HeadSHA: readyHead, Draft: draft,
		}},
	}
	scm := &fakeSCMAction{observation: ports.SCMObservation{
		Fetched: true,
		PR:      ports.SCMPRObservation{Number: 42, HeadSHA: readyHead, Draft: draft},
	}}
	setter := &draftSetterFake{}
	return store, scm, setter, NewActionService(ActionDeps{Store: store, Reader: scm, Draft: setter})
}

func TestSetReady_EnforcesCallerRole(t *testing.T) {
	cases := []struct {
		name      string
		requester domain.SessionID
		allowed   bool
	}{
		{"root", "root", true},
		{"owning coordinator", "coord", true},
		{"anonymous caller", "", true},
		{"worker itself", "worker", false},
		{"other coordinator", "other", false},
		{"terminated root", "dead", false},
		{"unknown session", "ghost", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store, _, setter, svc := readyFixture(true)
			res, err := svc.SetReady(context.Background(), ReadyRequest{SessionID: "worker", Ready: true, RequestedBySessionID: tc.requester})
			if tc.allowed {
				if err != nil || !res.Changed || len(setter.requests) != 1 {
					t.Fatalf("res = %+v err = %v requests = %d, want one flip", res, err, len(setter.requests))
				}
				if got := setter.requests[0]; got.Draft || got.PR.Number != 42 {
					t.Fatalf("request = %+v, want ready PR 42", got)
				}
				if len(store.events) != 1 {
					t.Fatalf("events = %d, want 1", len(store.events))
				}
				ev := store.events[0]
				if ev.ActorSessionID != tc.requester || ev.SessionID != "worker" || ev.HeadSHA != readyHead || !ev.Ready || ev.PRURL == "" {
					t.Fatalf("event = %+v", ev)
				}
				return
			}
			var apiErr *apierr.Error
			if !errors.As(err, &apiErr) || apiErr.Kind != apierr.KindForbidden || apiErr.Code != "CI_GATE_FORBIDDEN" {
				t.Fatalf("err = %v, want 403 CI_GATE_FORBIDDEN", err)
			}
			if len(setter.requests) != 0 || len(store.events) != 0 {
				t.Fatalf("a refused caller changed state: %d flips, %d events", len(setter.requests), len(store.events))
			}
		})
	}
}

func TestSetReady_UndoConvertsToDraft(t *testing.T) {
	store, _, setter, svc := readyFixture(false)
	res, err := svc.SetReady(context.Background(), ReadyRequest{SessionID: "worker", Ready: false, RequestedBySessionID: "root"})
	if err != nil || !res.Changed || res.Ready {
		t.Fatalf("res = %+v err = %v", res, err)
	}
	if len(setter.requests) != 1 || !setter.requests[0].Draft {
		t.Fatalf("requests = %+v, want one convert-to-draft", setter.requests)
	}
	if len(store.events) != 1 || store.events[0].Ready {
		t.Fatalf("events = %+v, want one undo event", store.events)
	}
}

func TestSetReady_AlreadyInRequestedStateIsNoOp(t *testing.T) {
	for _, tc := range []struct {
		name  string
		draft bool
		ready bool
	}{{"ready already", false, true}, {"draft already", true, false}} {
		t.Run(tc.name, func(t *testing.T) {
			store, _, setter, svc := readyFixture(tc.draft)
			res, err := svc.SetReady(context.Background(), ReadyRequest{SessionID: "worker", Ready: tc.ready, RequestedBySessionID: "root"})
			if err != nil || res.Changed || res.PRNumber != 42 {
				t.Fatalf("res = %+v err = %v, want an unchanged success", res, err)
			}
			if len(setter.requests) != 0 || len(store.events) != 0 {
				t.Fatalf("no-op wrote state: %d flips, %d events", len(setter.requests), len(store.events))
			}
		})
	}
}

func TestSetReady_NoOpenPRFails(t *testing.T) {
	cases := map[string]func(*readyStoreFake, *fakeSCMAction){
		"no tracked PR":      func(s *readyStoreFake, _ *fakeSCMAction) { s.prs = nil },
		"merged PR":          func(s *readyStoreFake, _ *fakeSCMAction) { s.prs[0].Merged = true },
		"closed on provider": func(_ *readyStoreFake, scm *fakeSCMAction) { scm.observation.PR.Closed = true },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			store, scm, setter, svc := readyFixture(true)
			mutate(store, scm)
			_, err := svc.SetReady(context.Background(), ReadyRequest{SessionID: "worker", Ready: true, RequestedBySessionID: "root"})
			var apiErr *apierr.Error
			if !errors.As(err, &apiErr) || apiErr.Kind != apierr.KindConflict || apiErr.Code != "NO_OPEN_PR" {
				t.Fatalf("err = %v, want 409 NO_OPEN_PR", err)
			}
			if len(setter.requests) != 0 {
				t.Fatalf("flips = %d, want 0", len(setter.requests))
			}
		})
	}
}

func TestSetReady_UnknownSessionAndProviderFailure(t *testing.T) {
	_, _, _, svc := readyFixture(true)
	_, err := svc.SetReady(context.Background(), ReadyRequest{SessionID: "ghost", Ready: true})
	var apiErr *apierr.Error
	if !errors.As(err, &apiErr) || apiErr.Kind != apierr.KindNotFound {
		t.Fatalf("err = %v, want 404", err)
	}

	store, _, setter, svc := readyFixture(true)
	setter.err = errors.New("boom")
	if _, err := svc.SetReady(context.Background(), ReadyRequest{SessionID: "worker", Ready: true, RequestedBySessionID: "root"}); err == nil {
		t.Fatal("provider failure was swallowed")
	}
	if len(store.events) != 0 {
		t.Fatalf("events = %d, want none after a failed flip", len(store.events))
	}
}
