package controllers_test

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/config"
	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/apierr"
	coordownership "github.com/aoagents/agent-orchestrator/backend/internal/service/ownership"
	prsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/pr"
)

type fakePRService struct {
	mergeResult   prsvc.MergeResult
	mergeErr      error
	mergeRequest  prsvc.MergeRequest
	resolveResult prsvc.ResolveResult
	resolveErr    error
	readyResult   prsvc.ReadyResult
	readyErr      error
	readyRequest  prsvc.ReadyRequest
}

func (f *fakePRService) SetReady(_ context.Context, request prsvc.ReadyRequest) (prsvc.ReadyResult, error) {
	f.readyRequest = request
	return f.readyResult, f.readyErr
}

func (f *fakePRService) Merge(_ context.Context, request prsvc.MergeRequest) (prsvc.MergeResult, error) {
	f.mergeRequest = request
	return f.mergeResult, f.mergeErr
}

func (f *fakePRService) ResolveComments(_ context.Context, _ string, _ []string) (prsvc.ResolveResult, error) {
	return f.resolveResult, f.resolveErr
}

func newPRTestServer(t *testing.T, svc prsvc.ActionManager) *httptest.Server {
	t.Helper()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv := httptest.NewServer(httpd.NewRouterWithControl(config.Config{}, log, nil, httpd.APIDeps{PRs: svc}, httpd.ControlDeps{}))
	t.Cleanup(srv.Close)
	return srv
}

const validMergeBody = `{"prUrl":"https://github.com/acme/widgets/pull/42","expectedHeadSha":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}`

// ---- Nil service → 503 SCM_NOT_CONFIGURED ----

func TestPRsRoutes_NilService_MergeReturns501(t *testing.T) {
	srv := newPRTestServer(t, nil)
	body, status, headers := doRequest(t, srv, "POST", "/api/v1/prs/1/merge", "")
	assertJSON(t, headers)
	assertErrorCode(t, body, status, http.StatusNotImplemented, "NOT_IMPLEMENTED")
}

func TestPRsRoutes_NilService_ResolveCommentsReturns501(t *testing.T) {
	srv := newPRTestServer(t, nil)
	body, status, headers := doRequest(t, srv, "POST", "/api/v1/prs/1/resolve-comments", "")
	assertJSON(t, headers)
	assertErrorCode(t, body, status, http.StatusNotImplemented, "NOT_IMPLEMENTED")
}

// ---- Merge: 200 ----

func TestPRsRoutes_Merge_200(t *testing.T) {
	svc := &fakePRService{mergeResult: prsvc.MergeResult{PRNumber: 42, Method: "squash"}}
	srv := newPRTestServer(t, svc)

	body, status, _ := doRequest(t, srv, "POST", "/api/v1/prs/42/merge", validMergeBody)
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", status, body)
	}
	var resp struct {
		OK       bool   `json:"ok"`
		PRNumber int    `json:"prNumber"`
		Method   string `json:"method"`
	}
	mustJSON(t, body, &resp)
	if !resp.OK || resp.PRNumber != 42 || resp.Method != "squash" {
		t.Errorf("resp = %+v, want {ok:true prNumber:42 method:squash}", resp)
	}
	if svc.mergeRequest.PRID != "42" || svc.mergeRequest.PRURL != "https://github.com/acme/widgets/pull/42" || svc.mergeRequest.ExpectedHeadSHA != "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" {
		t.Fatalf("merge request = %#v", svc.mergeRequest)
	}
}

func TestPRsRoutes_Merge_ForwardsRequestingSession(t *testing.T) {
	svc := &fakePRService{mergeResult: prsvc.MergeResult{PRNumber: 42, Method: "squash"}}
	srv := newPRTestServer(t, svc)
	body := `{"prUrl":"https://github.com/acme/widgets/pull/42","expectedHeadSha":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","requestedBySessionId":" ao-root "}`

	respBody, status, _ := doRequest(t, srv, "POST", "/api/v1/prs/42/merge", body)
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", status, respBody)
	}
	if svc.mergeRequest.RequestedBySessionID != "ao-root" {
		t.Fatalf("requester = %q, want ao-root", svc.mergeRequest.RequestedBySessionID)
	}
}

func TestPRsRoutes_Merge_AnonymousCallerHasNoRequester(t *testing.T) {
	svc := &fakePRService{mergeResult: prsvc.MergeResult{PRNumber: 42, Method: "squash"}}
	srv := newPRTestServer(t, svc)

	_, status, _ := doRequest(t, srv, "POST", "/api/v1/prs/42/merge", validMergeBody)
	if status != http.StatusOK || svc.mergeRequest.RequestedBySessionID != "" {
		t.Fatalf("status = %d, requester = %q", status, svc.mergeRequest.RequestedBySessionID)
	}
}

// A live coordinator or worker is refused with the ADR 0010 code in the
// standard envelope, and the caller's request id survives.
func TestPRsRoutes_Merge_403MergeRootOnly(t *testing.T) {
	for _, kind := range []domain.SessionKind{domain.KindCoordinator, domain.KindWorker} {
		t.Run(string(kind), func(t *testing.T) {
			svc := &fakePRService{mergeErr: fmt.Errorf("merge: %w", coordownership.RequireMergeAuthority(
				domain.SessionRecord{ID: "ao-9", Kind: kind}))}
			srv := newPRTestServer(t, svc)
			req, err := http.NewRequest(http.MethodPost, srv.URL+"/api/v1/prs/42/merge",
				strings.NewReader(`{"requestedBySessionId":"ao-9"}`))
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-Request-Id", "req-merge-1")
			resp, err := srv.Client().Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			body, _ := io.ReadAll(resp.Body)
			assertJSON(t, resp.Header)
			assertErrorCode(t, body, resp.StatusCode, http.StatusForbidden, "MERGE_ROOT_ONLY")
			var env struct {
				Error     string         `json:"error"`
				RequestID string         `json:"requestId"`
				Details   map[string]any `json:"details"`
			}
			mustJSON(t, body, &env)
			if env.Error != "forbidden" || env.RequestID != "req-merge-1" || env.Details["sessionId"] != "ao-9" {
				t.Fatalf("envelope = %+v", env)
			}
		})
	}
}

func TestPRsRoutes_Merge_BlankIdentityIs400(t *testing.T) {
	svc := &fakePRService{mergeErr: prsvc.ErrInvalidPR}
	srv := newPRTestServer(t, svc)
	body, status, _ := doRequest(t, srv, "POST", "/api/v1/prs/42/merge", `{}`)
	assertErrorCode(t, body, status, http.StatusBadRequest, "INVALID_PR")
}

// ---- Merge: 404 ----

func TestPRsRoutes_Merge_404(t *testing.T) {
	svc := &fakePRService{mergeErr: prsvc.ErrPRNotFound}
	srv := newPRTestServer(t, svc)

	body, status, headers := doRequest(t, srv, "POST", "/api/v1/prs/99/merge", validMergeBody)
	assertJSON(t, headers)
	assertErrorCode(t, body, status, http.StatusNotFound, "PR_NOT_FOUND")
}

// ---- Merge: 409 ----

func TestPRsRoutes_Merge_409(t *testing.T) {
	svc := &fakePRService{mergeErr: prsvc.ErrPRNotMergeable}
	srv := newPRTestServer(t, svc)

	body, status, headers := doRequest(t, srv, "POST", "/api/v1/prs/1/merge", validMergeBody)
	assertJSON(t, headers)
	assertErrorCode(t, body, status, http.StatusConflict, "PR_NOT_MERGEABLE")
}

// ---- Merge: 422 ----

func TestPRsRoutes_Merge_422(t *testing.T) {
	svc := &fakePRService{mergeErr: prsvc.ErrPRPreconditions}
	srv := newPRTestServer(t, svc)

	body, status, headers := doRequest(t, srv, "POST", "/api/v1/prs/1/merge", validMergeBody)
	assertJSON(t, headers)
	assertErrorCode(t, body, status, http.StatusUnprocessableEntity, "PR_PRECONDITIONS_UNMET")
}

// ---- ResolveComments: 200 ----

func TestPRsRoutes_ResolveComments_200(t *testing.T) {
	svc := &fakePRService{resolveResult: prsvc.ResolveResult{Resolved: 3}}
	srv := newPRTestServer(t, svc)

	body, status, _ := doRequest(t, srv, "POST", "/api/v1/prs/42/resolve-comments", `{"commentIds":["T_1","T_2","T_3"]}`)
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", status, body)
	}
	var resp struct {
		OK       bool `json:"ok"`
		Resolved int  `json:"resolved"`
	}
	mustJSON(t, body, &resp)
	if !resp.OK || resp.Resolved != 3 {
		t.Errorf("resp = %+v, want {ok:true resolved:3}", resp)
	}
}

func TestPRsRoutes_ResolveComments_200_NoBody(t *testing.T) {
	svc := &fakePRService{resolveResult: prsvc.ResolveResult{Resolved: 2}}
	srv := newPRTestServer(t, svc)

	body, status, _ := doRequest(t, srv, "POST", "/api/v1/prs/42/resolve-comments", "")
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", status, body)
	}
}

// ---- ResolveComments: 404 ----

func TestPRsRoutes_ResolveComments_404(t *testing.T) {
	svc := &fakePRService{resolveErr: prsvc.ErrPRNotFound}
	srv := newPRTestServer(t, svc)

	body, status, headers := doRequest(t, srv, "POST", "/api/v1/prs/99/resolve-comments", "")
	assertJSON(t, headers)
	assertErrorCode(t, body, status, http.StatusNotFound, "PR_NOT_FOUND")
}

// ---- ResolveComments: 422 ----

func TestPRsRoutes_ResolveComments_422(t *testing.T) {
	svc := &fakePRService{resolveErr: prsvc.ErrNothingToResolve}
	srv := newPRTestServer(t, svc)

	body, status, headers := doRequest(t, srv, "POST", "/api/v1/prs/1/resolve-comments", "")
	assertJSON(t, headers)
	assertErrorCode(t, body, status, http.StatusUnprocessableEntity, "NOTHING_TO_RESOLVE")
}

func TestPRsRoutes_ResolveComments_InvalidPRID(t *testing.T) {
	svc := &fakePRService{resolveResult: prsvc.ResolveResult{Resolved: 0}}
	srv := newPRTestServer(t, svc)

	for _, id := range []string{"0", "abc", "-1"} {
		body, status, headers := doRequest(t, srv, "POST", "/api/v1/prs/"+id+"/resolve-comments", "")
		assertJSON(t, headers)
		assertErrorCode(t, body, status, http.StatusBadRequest, "INVALID_PR")
	}
}

// ---- Ready: route, requester forwarding, error envelope ----

func TestPRsRoutes_Ready_200ForwardsRequest(t *testing.T) {
	svc := &fakePRService{readyResult: prsvc.ReadyResult{PRURL: "u", PRNumber: 42, HeadSHA: "abc", Ready: true, Changed: true}}
	srv := newPRTestServer(t, svc)

	body, status, _ := doRequest(t, srv, "POST", "/api/v1/sessions/demo-1/pr/ready", `{"ready":true,"requestedBySessionId":" ao-root "}`)
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", status, body)
	}
	var resp struct {
		OK       bool `json:"ok"`
		PRNumber int  `json:"prNumber"`
		Ready    bool `json:"ready"`
		Changed  bool `json:"changed"`
	}
	mustJSON(t, body, &resp)
	if !resp.OK || resp.PRNumber != 42 || !resp.Ready || !resp.Changed {
		t.Fatalf("resp = %+v", resp)
	}
	if got := svc.readyRequest; got.SessionID != "demo-1" || !got.Ready || got.RequestedBySessionID != "ao-root" {
		t.Fatalf("request = %+v", got)
	}
}

func TestPRsRoutes_Ready_ForbiddenEnvelope(t *testing.T) {
	svc := &fakePRService{readyErr: apierr.New(apierr.KindForbidden, "CI_GATE_FORBIDDEN", "no", nil)}
	srv := newPRTestServer(t, svc)

	body, status, _ := doRequest(t, srv, "POST", "/api/v1/sessions/demo-1/pr/ready", `{"ready":true,"requestedBySessionId":"w"}`)
	assertErrorCode(t, body, status, http.StatusForbidden, "CI_GATE_FORBIDDEN")
}

func TestPRsRoutes_NilService_ReadyReturns501(t *testing.T) {
	srv := newPRTestServer(t, nil)
	body, status, _ := doRequest(t, srv, "POST", "/api/v1/sessions/demo-1/pr/ready", `{"ready":true}`)
	assertErrorCode(t, body, status, http.StatusNotImplemented, "NOT_IMPLEMENTED")
}
