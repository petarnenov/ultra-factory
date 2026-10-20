package controllers_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd"
	"github.com/aoagents/agent-orchestrator/backend/internal/service/remoteworker"
	reportsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/report"
	sessionsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/session"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite"
)

type ownershipFixture struct {
	store       *sqlite.Store
	root        domain.SessionRecord
	coordinator domain.SessionRecord
	otherCoord  domain.SessionRecord
	worker      domain.SessionRecord
	rootWorker  domain.SessionRecord
	otherCoord2 domain.SessionRecord
	now         time.Time
}

func newOwnershipFixture(t *testing.T) ownershipFixture {
	t.Helper()
	ctx := context.Background()
	store, err := sqlite.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	for _, project := range []string{"ao", "other"} {
		if err := store.UpsertProject(ctx, domain.ProjectRecord{
			ID: project, Path: "/tmp/" + project, RegisteredAt: time.Now().UTC(),
		}); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now().UTC().Truncate(time.Second)
	create := func(project string, kind domain.SessionKind, scope string, owner *domain.SessionID) domain.SessionRecord {
		rec, err := store.CreateSession(ctx, domain.SessionRecord{
			ProjectID: domain.ProjectID(project), Kind: kind, CoordinatorScope: scope,
			OwnerSessionID: owner, Harness: domain.HarnessClaudeCode,
			Activity:  domain.Activity{State: domain.ActivityIdle, LastActivityAt: now},
			CreatedAt: now, UpdatedAt: now,
		})
		if err != nil {
			t.Fatal(err)
		}
		return rec
	}
	root := create("ao", domain.KindOrchestrator, "", nil)
	coordinator := create("ao", domain.KindCoordinator, "auth", nil)
	otherCoord := create("ao", domain.KindCoordinator, "docs", nil)
	worker := create("ao", domain.KindWorker, "", nil)
	rootWorker := create("ao", domain.KindWorker, "", nil)
	otherCoord2 := create("other", domain.KindCoordinator, "remote", nil)
	return ownershipFixture{
		store: store, root: root, coordinator: coordinator, otherCoord: otherCoord,
		worker: worker, rootWorker: rootWorker, otherCoord2: otherCoord2, now: now,
	}
}

func (f ownershipFixture) server(t *testing.T) *httptest.Server {
	t.Helper()
	return newRouterServer(t, httpd.APIDeps{
		Sessions: sessionsvc.NewWithDeps(sessionsvc.Deps{Store: f.store}),
	})
}

func (f ownershipFixture) linkServer(t *testing.T) *httptest.Server {
	t.Helper()
	links := remoteworker.New(remoteworker.Deps{
		Store: f.store, SelfHostID: "host-local", Now: func() time.Time { return f.now },
	})
	return newRouterServer(t, httpd.APIDeps{
		Sessions:          sessionsvc.NewWithDeps(sessionsvc.Deps{Store: f.store}),
		RemoteWorkerLinks: links,
	})
}

type sessionOwnershipBody struct {
	SessionID        string  `json:"sessionId"`
	ProjectID        string  `json:"projectId"`
	Kind             string  `json:"kind"`
	OwnerSessionID   *string `json:"ownerSessionId"`
	OwnerKind        string  `json:"ownerKind"`
	OwnerScope       string  `json:"ownerScope"`
	CoordinatorScope string  `json:"coordinatorScope"`
	RootSessionID    *string `json:"rootSessionId"`
}

func TestSessionOwnershipReadExposesOwnerScopeAndRoot(t *testing.T) {
	f := newOwnershipFixture(t)
	srv := f.server(t)

	body, status, _ := doRequest(t, srv, http.MethodGet, "/api/v1/sessions/"+string(f.worker.ID)+"/ownership", "")
	var got sessionOwnershipBody
	if err := json.Unmarshal(body, &got); err != nil || status != http.StatusOK {
		t.Fatalf("status=%d body=%s err=%v", status, body, err)
	}
	if got.SessionID != string(f.worker.ID) || got.Kind != "worker" || got.ProjectID != "ao" {
		t.Fatalf("identity = %+v", got)
	}
	if got.OwnerSessionID != nil {
		t.Fatalf("a fresh worker must read as root-owned, owner=%v", got.OwnerSessionID)
	}
	if got.RootSessionID == nil || *got.RootSessionID != string(f.root.ID) {
		t.Fatalf("rootSessionId = %v, want %s", got.RootSessionID, f.root.ID)
	}

	if _, _, err := f.store.SetSessionOwner(context.Background(), f.worker.ID, &f.coordinator.ID, domain.OwnershipExplicitAssignment, f.now, false); err != nil {
		t.Fatal(err)
	}
	body, status, _ = doRequest(t, srv, http.MethodGet, "/api/v1/sessions/"+string(f.worker.ID)+"/ownership", "")
	if err := json.Unmarshal(body, &got); err != nil || status != http.StatusOK {
		t.Fatalf("status=%d body=%s err=%v", status, body, err)
	}
	if got.OwnerSessionID == nil || *got.OwnerSessionID != string(f.coordinator.ID) {
		t.Fatalf("ownerSessionId = %v, want %s", got.OwnerSessionID, f.coordinator.ID)
	}
	if got.OwnerKind != "coordinator" || got.OwnerScope != "auth" {
		t.Fatalf("owner detail = %+v", got)
	}

	body, status, _ = doRequest(t, srv, http.MethodGet, "/api/v1/sessions/"+string(f.coordinator.ID)+"/ownership", "")
	if err := json.Unmarshal(body, &got); err != nil || status != http.StatusOK {
		t.Fatalf("coordinator status=%d body=%s err=%v", status, body, err)
	}
	if got.Kind != "coordinator" || got.CoordinatorScope != "auth" || got.OwnerSessionID != nil {
		t.Fatalf("coordinator ownership = %s", body)
	}

	body, status, _ = doRequest(t, srv, http.MethodGet, "/api/v1/sessions/ghost/ownership", "")
	if status != http.StatusNotFound || !reportContainsAll(string(body), "SESSION_NOT_FOUND", "requestId") {
		t.Fatalf("unknown session: status=%d body=%s", status, body)
	}
}

func TestCoordinatorRosterListsOwnedAndRootOwnedWorkers(t *testing.T) {
	f := newOwnershipFixture(t)
	srv := f.server(t)
	if _, _, err := f.store.SetSessionOwner(context.Background(), f.worker.ID, &f.coordinator.ID, domain.OwnershipExplicitAssignment, f.now, false); err != nil {
		t.Fatal(err)
	}

	body, status, _ := doRequest(t, srv, http.MethodGet, "/api/v1/projects/ao/coordinators", "")
	var got struct {
		ProjectID     string  `json:"projectId"`
		RootSessionID *string `json:"rootSessionId"`
		Coordinators  []struct {
			CoordinatorSessionID string   `json:"coordinatorSessionId"`
			CoordinatorScope     string   `json:"coordinatorScope"`
			IsTerminated         bool     `json:"isTerminated"`
			WorkerSessionIDs     []string `json:"workerSessionIds"`
			WorkerCount          int      `json:"workerCount"`
		} `json:"coordinators"`
		RootWorkerSessionIDs []string `json:"rootWorkerSessionIds"`
	}
	if err := json.Unmarshal(body, &got); err != nil || status != http.StatusOK {
		t.Fatalf("status=%d body=%s err=%v", status, body, err)
	}
	if got.ProjectID != "ao" || got.RootSessionID == nil || *got.RootSessionID != string(f.root.ID) {
		t.Fatalf("roster = %s", body)
	}
	if len(got.Coordinators) != 2 {
		t.Fatalf("coordinators = %+v, want 2 live", got.Coordinators)
	}
	if got.Coordinators[0].CoordinatorSessionID != string(f.coordinator.ID) ||
		got.Coordinators[0].CoordinatorScope != "auth" || got.Coordinators[0].WorkerCount != 1 ||
		len(got.Coordinators[0].WorkerSessionIDs) != 1 || got.Coordinators[0].WorkerSessionIDs[0] != string(f.worker.ID) {
		t.Fatalf("coordinator entry = %+v", got.Coordinators[0])
	}
	if len(got.RootWorkerSessionIDs) != 1 || got.RootWorkerSessionIDs[0] != string(f.rootWorker.ID) {
		t.Fatalf("root workers = %+v", got.RootWorkerSessionIDs)
	}

	body, status, _ = doRequest(t, srv, http.MethodGet, "/api/v1/projects/ghost/coordinators", "")
	if status != http.StatusNotFound || !reportContainsAll(string(body), "PROJECT_NOT_FOUND", "requestId") {
		t.Fatalf("unknown project: status=%d body=%s", status, body)
	}
	body, status, _ = doRequest(t, srv, http.MethodGet, "/api/v1/projects/ao/coordinators?includeTerminated=maybe", "")
	if status != http.StatusBadRequest || !reportContainsAll(string(body), "INVALID_QUERY", "requestId") {
		t.Fatalf("invalid query: status=%d body=%s", status, body)
	}
}

func TestSessionOwnerWriteAssignsReassignsAndClears(t *testing.T) {
	f := newOwnershipFixture(t)
	srv := f.server(t)

	put := func(owner any) (string, int) {
		t.Helper()
		payload, _ := json.Marshal(map[string]any{
			"ownerSessionId":       owner,
			"requestedBySessionId": string(f.root.ID),
		})
		body, status, _ := doRequest(t, srv, http.MethodPut, "/api/v1/sessions/"+string(f.worker.ID)+"/owner", string(payload))
		return string(body), status
	}

	body, status := put(string(f.otherCoord.ID))
	if status != http.StatusOK || !reportContainsAll(body, `"ownerSessionId":"`+string(f.otherCoord.ID)+`"`) {
		t.Fatalf("reassign status=%d body=%s", status, body)
	}
	body, status = put(string(f.coordinator.ID))
	if status != http.StatusOK || !reportContainsAll(body, `"ownerSessionId":"`+string(f.coordinator.ID)+`"`) {
		t.Fatalf("assign status=%d body=%s", status, body)
	}
	body, status = put(nil)
	if status != http.StatusOK || !reportContainsAll(body, `"ownerSessionId":null`) {
		t.Fatalf("clear status=%d body=%s", status, body)
	}
}

func TestSessionOwnerWriteRejectionsUseADRErrorCodes(t *testing.T) {
	f := newOwnershipFixture(t)
	srv := f.server(t)
	ownerBody := func(owner any, caller domain.SessionID) string {
		payload, _ := json.Marshal(map[string]any{"ownerSessionId": owner, "requestedBySessionId": string(caller)})
		return string(payload)
	}
	workerPath := "/api/v1/sessions/" + string(f.worker.ID) + "/owner"

	terminated := f.otherCoord
	rec, ok, err := f.store.GetSession(context.Background(), terminated.ID)
	if err != nil || !ok {
		t.Fatalf("load coordinator: %v %v", ok, err)
	}
	rec.IsTerminated = true
	rec.Activity.State = domain.ActivityExited
	if err := f.store.UpdateSession(context.Background(), rec); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name   string
		path   string
		body   string
		status int
		code   string
	}{
		{"non-root caller", workerPath, ownerBody(nil, f.coordinator.ID), http.StatusForbidden, "OWNERSHIP_ROOT_ONLY"},
		{"root target", "/api/v1/sessions/" + string(f.root.ID) + "/owner", ownerBody(f.coordinator.ID, f.root.ID), http.StatusBadRequest, "OWNER_TARGET_NOT_WORKER"},
		{"root is not a coordinator", workerPath, ownerBody(f.root.ID, f.root.ID), http.StatusConflict, "OWNER_NOT_COORDINATOR"},
		{"cross-project owner", workerPath, ownerBody(f.otherCoord2.ID, f.root.ID), http.StatusConflict, "OWNER_CROSS_PROJECT"},
		{"terminated owner", workerPath, ownerBody(terminated.ID, f.root.ID), http.StatusConflict, "OWNER_TERMINATED"},
		{"missing worker", "/api/v1/sessions/ghost/owner", ownerBody(nil, f.root.ID), http.StatusNotFound, "SESSION_NOT_FOUND"},
		{"missing caller", workerPath, ownerBody(nil, "ghost"), http.StatusNotFound, "SESSION_NOT_FOUND"},
		{"invalid json", workerPath, `{"ownerSessionId":`, http.StatusBadRequest, "INVALID_JSON"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body, status, _ := doRequest(t, srv, http.MethodPut, tc.path, tc.body)
			if status != tc.status || !reportContainsAll(string(body), tc.code, "requestId") {
				t.Fatalf("status=%d body=%s, want %d %s", status, body, tc.status, tc.code)
			}
		})
	}

	// A claimed report blocks an explicit owner change until the claim settles.
	reports := reportsvc.New(reportsvc.Deps{Store: f.store})
	if _, err := reports.Create(context.Background(), reportsvc.CreateInput{SessionID: f.worker.ID, State: domain.ReportCheckpoint, Note: "in flight"}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.ClaimPendingReportBatch(context.Background(), "ao", f.root.ID, f.root.ID, "claim-token", f.now); err != nil {
		t.Fatal(err)
	}
	body, status, _ := doRequest(t, srv, http.MethodPut, workerPath, ownerBody(f.coordinator.ID, f.root.ID))
	if status != http.StatusConflict || !reportContainsAll(string(body), "OWNER_REASSIGNMENT_IN_FLIGHT", "requestId") {
		t.Fatalf("claimed report: status=%d body=%s", status, body)
	}
}

func TestRemoteWorkerOwnerRoutesExposeAndWriteOwnership(t *testing.T) {
	f := newOwnershipFixture(t)
	srv := f.linkServer(t)
	link := domain.RemoteWorkerLink{
		HostID: "host-build", HostLabel: "build-box", RemoteSessionID: "widgets-3",
		RemoteProjectID: "widgets", ProjectID: "ao", DisplayName: "auth refactor", CreatedAt: f.now,
	}
	if err := f.store.UpsertRemoteWorkerLink(context.Background(), link); err != nil {
		t.Fatal(err)
	}

	body, status, _ := doRequest(t, srv, http.MethodGet, "/api/v1/remote-worker-links?project=ao", "")
	var listed struct {
		Links []struct {
			HostID          string  `json:"hostId"`
			RemoteSessionID string  `json:"remoteSessionId"`
			OwnerSessionID  *string `json:"ownerSessionId"`
		} `json:"links"`
	}
	if err := json.Unmarshal(body, &listed); err != nil || status != http.StatusOK {
		t.Fatalf("list status=%d body=%s err=%v", status, body, err)
	}
	if len(listed.Links) != 1 || listed.Links[0].OwnerSessionID != nil {
		t.Fatalf("links = %s, want one root-owned link", body)
	}

	payload := func(owner any, caller domain.SessionID) string {
		raw, _ := json.Marshal(map[string]any{"ownerSessionId": owner, "requestedBySessionId": string(caller)})
		return string(raw)
	}
	path := "/api/v1/remote-worker-links/host-build/widgets-3/owner"
	body, status, _ = doRequest(t, srv, http.MethodPut, path, payload(f.coordinator.ID, f.root.ID))
	if status != http.StatusOK || !reportContainsAll(string(body), `"ownerSessionId":"`+string(f.coordinator.ID)+`"`) {
		t.Fatalf("assign status=%d body=%s", status, body)
	}
	body, status, _ = doRequest(t, srv, http.MethodPut, path, payload(nil, f.root.ID))
	if status != http.StatusOK || !reportContainsAll(string(body), `"ownerSessionId":null`) {
		t.Fatalf("clear status=%d body=%s", status, body)
	}

	for _, tc := range []struct {
		name   string
		path   string
		body   string
		status int
		code   string
	}{
		{"non-root caller", path, payload(nil, f.coordinator.ID), http.StatusForbidden, "OWNERSHIP_ROOT_ONLY"},
		{"cross-project owner", path, payload(f.otherCoord2.ID, f.root.ID), http.StatusConflict, "OWNER_CROSS_PROJECT"},
		{"root is not a coordinator", path, payload(f.root.ID, f.root.ID), http.StatusConflict, "OWNER_NOT_COORDINATOR"},
		{"missing link", "/api/v1/remote-worker-links/host-build/ghost/owner", payload(nil, f.root.ID), http.StatusNotFound, "REMOTE_WORKER_LINK_NOT_FOUND"},
		{"invalid json", path, `{"ownerSessionId":`, http.StatusBadRequest, "INVALID_JSON"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body, status, _ := doRequest(t, srv, http.MethodPut, tc.path, tc.body)
			if status != tc.status || !reportContainsAll(string(body), tc.code, "requestId") {
				t.Fatalf("status=%d body=%s, want %d %s", status, body, tc.status, tc.code)
			}
		})
	}
}

func TestSessionListExposesOwnerAndScopeWithExplicitNullForRootOwned(t *testing.T) {
	f := newOwnershipFixture(t)
	srv := f.server(t)
	if _, _, err := f.store.SetSessionOwner(context.Background(), f.worker.ID, &f.coordinator.ID, domain.OwnershipExplicitAssignment, f.now, false); err != nil {
		t.Fatal(err)
	}

	body, status, _ := doRequest(t, srv, http.MethodGet, "/api/v1/sessions?project=ao", "")
	var list struct {
		Sessions []map[string]json.RawMessage `json:"sessions"`
	}
	if err := json.Unmarshal(body, &list); err != nil || status != http.StatusOK {
		t.Fatalf("status=%d body=%s err=%v", status, body, err)
	}
	byID := map[string]map[string]json.RawMessage{}
	for _, sess := range list.Sessions {
		var id string
		_ = json.Unmarshal(sess["id"], &id)
		byID[id] = sess
	}
	owned := byID[string(f.worker.ID)]
	if string(owned["ownerSessionId"]) != `"`+string(f.coordinator.ID)+`"` {
		t.Fatalf("owned worker ownerSessionId = %s", owned["ownerSessionId"])
	}
	rootOwned, present := byID[string(f.rootWorker.ID)]["ownerSessionId"]
	if !present || string(rootOwned) != "null" {
		t.Fatalf("root-owned worker must carry an explicit null, got %q present=%v", rootOwned, present)
	}
	if string(byID[string(f.coordinator.ID)]["coordinatorScope"]) != `"auth"` {
		t.Fatalf("coordinator scope = %s", byID[string(f.coordinator.ID)]["coordinatorScope"])
	}
}

func TestCoordinatorRosterCarriesLatestSummaryAndRecoveryFlag(t *testing.T) {
	f := newOwnershipFixture(t)
	ctx := context.Background()
	summaries := reportsvc.NewSummaryService(reportsvc.SummaryDeps{Store: f.store})
	srv := newRouterServer(t, httpd.APIDeps{
		Sessions:  sessionsvc.NewWithDeps(sessionsvc.Deps{Store: f.store}),
		Summaries: summaries,
	})
	if _, _, err := f.store.SetSessionOwner(ctx, f.worker.ID, &f.coordinator.ID, domain.OwnershipExplicitAssignment, f.now, false); err != nil {
		t.Fatal(err)
	}
	if _, _, err := summaries.Submit(ctx, reportsvc.SummaryInput{
		CoordinatorSessionID: f.coordinator.ID, SubmissionKey: "s1", State: domain.SummaryWorking,
		WorkerSessionIDs: []domain.SessionID{f.worker.ID}, Result: "auth half done",
	}); err != nil {
		t.Fatal(err)
	}

	type rosterBody struct {
		Coordinators []struct {
			CoordinatorSessionID string `json:"coordinatorSessionId"`
			NeedsRecovery        bool   `json:"needsRecovery"`
			LatestSummary        *struct {
				State  string `json:"state"`
				Result string `json:"result"`
			} `json:"latestSummary"`
		} `json:"coordinators"`
	}
	read := func() (rosterBody, []byte) {
		body, status, _ := doRequest(t, srv, http.MethodGet, "/api/v1/projects/ao/coordinators", "")
		var got rosterBody
		if err := json.Unmarshal(body, &got); err != nil || status != http.StatusOK {
			t.Fatalf("status=%d body=%s err=%v", status, body, err)
		}
		return got, body
	}
	got, body := read()
	if len(got.Coordinators) != 2 {
		t.Fatalf("roster = %s", body)
	}
	with, without := got.Coordinators[0], got.Coordinators[1]
	if with.CoordinatorSessionID != string(f.coordinator.ID) || with.LatestSummary == nil ||
		with.LatestSummary.State != "working" || with.LatestSummary.Result != "auth half done" || with.NeedsRecovery {
		t.Fatalf("coordinator with summary = %+v", with)
	}
	if without.LatestSummary != nil || !reportContainsAll(string(body), `"latestSummary":null`, `"needsRecovery":false`) {
		t.Fatalf("coordinator without summary must carry an explicit null: %s", body)
	}
}

func TestCoordinatorLimitsRouteUpdatesAndExposesTheLimit(t *testing.T) {
	f := newOwnershipFixture(t)
	srv := f.server(t)
	path := "/api/v1/sessions/" + string(f.coordinator.ID) + "/coordinator-limits"
	put := func(payload string) (string, int) {
		t.Helper()
		body, status, _ := doRequest(t, srv, http.MethodPut, path, payload)
		return string(body), status
	}

	body, status := put(`{"workerLimit":2,"requestedBySessionId":"` + string(f.root.ID) + `"}`)
	if status != http.StatusOK || !reportContainsAll(body, `"workerLimit":2`, `"activeWorkerCount":0`, `"maxWorkersPerCoordinator":3`) {
		t.Fatalf("root update status=%d body=%s", status, body)
	}
	body, status = put(`{"workerLimit":1,"operatorInitiated":true,"origin":"ui"}`)
	if status != http.StatusOK || !reportContainsAll(body, `"workerLimit":1`) {
		t.Fatalf("operator update status=%d body=%s", status, body)
	}
	events, err := f.store.ListCoordinatorLimitEvents(context.Background(), f.coordinator.ID)
	if err != nil || len(events) != 2 || events[1].Origin != "ui" || events[1].ActorSessionID != f.root.ID || events[0].Origin != "root" {
		t.Fatalf("audit = %+v err=%v", events, err)
	}

	body, status = put(`{"workerLimit":1,"requestedBySessionId":"` + string(f.otherCoord.ID) + `"}`)
	if status != http.StatusForbidden || !reportContainsAll(body, "COORDINATOR_ROOT_REQUIRED", "requestId") {
		t.Fatalf("non-root status=%d body=%s", status, body)
	}
	body, status = put(`{"workerLimit":9,"requestedBySessionId":"` + string(f.root.ID) + `"}`)
	if status != http.StatusBadRequest || !reportContainsAll(body, "COORDINATOR_WORKER_LIMIT_INVALID", `"max":3`) {
		t.Fatalf("above the project value status=%d body=%s", status, body)
	}
	for _, bad := range []string{
		`{"workerLimit":1,"operatorInitiated":true}`,
		`{"workerLimit":1,"operatorInitiated":true,"origin":"web"}`,
		`{"workerLimit":1,"origin":"ui","requestedBySessionId":"` + string(f.root.ID) + `"}`,
	} {
		body, status = put(bad)
		if status != http.StatusBadRequest || !reportContainsAll(body, "COORDINATOR_LIMITS_ORIGIN_INVALID") {
			t.Fatalf("%s: status=%d body=%s", bad, status, body)
		}
	}
	body, status = put(`{"workerLimit":1,"unknown":true}`)
	if status != http.StatusBadRequest || !reportContainsAll(body, "INVALID_JSON") {
		t.Fatalf("unknown field status=%d body=%s", status, body)
	}

	roster, _, _ := doRequest(t, srv, http.MethodGet, "/api/v1/projects/ao/coordinators", "")
	if !reportContainsAll(string(roster), `"capacity":{`, `"maxWorkers":6`, `"workerLimit":1`, `"workerLimit":3`, `"activeWorkerCount":0`) {
		t.Fatalf("roster = %s", roster)
	}
	view, _, _ := doRequest(t, srv, http.MethodGet, path[:len(path)-len("coordinator-limits")]+"ownership", "")
	if !reportContainsAll(string(view), `"workerLimit":1`, `"maxCoordinators":2`) {
		t.Fatalf("ownership = %s", view)
	}
}
