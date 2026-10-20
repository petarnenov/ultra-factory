package controllers_test

import (
	"net/http"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/apierr"
)

func TestSessionsAPI_SpawnCoordinatorForwardsScopeAndParent(t *testing.T) {
	svc := newFakeSessionService()
	srv := newSessionTestServer(t, svc)

	body, status, _ := doRequest(t, srv, "POST", "/api/v1/sessions",
		`{"projectId":"ao","kind":"coordinator","scope":"auth module","parentSessionId":"ao-1","displayName":"Auth"}`)
	if status != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body=%s", status, body)
	}
	got := svc.lastSpawn
	if got.Kind != domain.KindCoordinator || got.CoordinatorScope != "auth module" || got.ParentSessionID != "ao-1" {
		t.Fatalf("spawn cfg = %+v", got)
	}
}

func TestSessionsAPI_SpawnCoordinatorErrorsKeepTheEnvelopeAndRequestID(t *testing.T) {
	for _, tc := range []struct {
		name       string
		err        *apierr.Error
		wantStatus int
		wantDetail string
	}{
		{"capacity", apierr.Conflict("COORDINATOR_LIMIT_REACHED", "full", map[string]any{"projectId": "ao", "current": 2, "allowed": 2}), http.StatusConflict, "allowed"},
		{"worker capacity", apierr.Conflict("COORDINATOR_WORKER_LIMIT_REACHED", "full", map[string]any{"coordinatorId": "ao-2", "current": 3, "allowed": 3}), http.StatusConflict, "coordinatorId"},
		{"project capacity", apierr.Conflict("PROJECT_WORKER_LIMIT_REACHED", "full", map[string]any{"projectId": "ao", "current": 6, "allowed": 6}), http.StatusConflict, "current"},
		{"not root", apierr.New(apierr.KindForbidden, "COORDINATOR_ROOT_REQUIRED", "root only", map[string]any{"sessionId": "ao-3"}), http.StatusForbidden, "sessionId"},
		{"scope", apierr.Invalid("COORDINATOR_SCOPE_REQUIRED", "scope", nil), http.StatusBadRequest, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc := newFakeSessionService()
			svc.spawnErr = tc.err
			srv := newSessionTestServer(t, svc)

			body, status, _ := doRequest(t, srv, "POST", "/api/v1/sessions",
				`{"projectId":"ao","kind":"coordinator","scope":"x","parentSessionId":"ao-1","displayName":"C"}`)
			var got struct {
				Error     string         `json:"error"`
				Code      string         `json:"code"`
				RequestID string         `json:"requestId"`
				Details   map[string]any `json:"details"`
			}
			mustJSON(t, body, &got)
			if status != tc.wantStatus || got.Code != tc.err.Code || got.RequestID == "" || got.Error == "" {
				t.Fatalf("status=%d envelope=%+v body=%s", status, got, body)
			}
			if tc.wantDetail != "" {
				if _, ok := got.Details[tc.wantDetail]; !ok {
					t.Fatalf("details = %v, want key %q", got.Details, tc.wantDetail)
				}
			}
		})
	}
}

func TestSessionsAPI_SpawnCoordinatorForwardsWorkerLimit(t *testing.T) {
	svc := newFakeSessionService()
	srv := newSessionTestServer(t, svc)

	body, status, _ := doRequest(t, srv, "POST", "/api/v1/sessions",
		`{"projectId":"ao","kind":"coordinator","scope":"auth","parentSessionId":"ao-1","displayName":"Auth","workerLimit":2}`)
	if status != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body=%s", status, body)
	}
	if got := svc.lastSpawn.CoordinatorWorkerLimit; got == nil || *got != 2 {
		t.Fatalf("forwarded workerLimit = %v, want 2", got)
	}
	doRequest(t, srv, "POST", "/api/v1/sessions",
		`{"projectId":"ao","kind":"coordinator","scope":"auth","parentSessionId":"ao-1","displayName":"Auth"}`)
	if got := svc.lastSpawn.CoordinatorWorkerLimit; got != nil {
		t.Fatalf("an omitted workerLimit must stay nil, got %d", *got)
	}
}
