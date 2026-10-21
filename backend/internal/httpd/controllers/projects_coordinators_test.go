package controllers_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

type invalidJSONBody struct {
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details"`
}

func TestProjectsAPI_InvalidJSONNamesRejectedField(t *testing.T) {
	srv := newTestServer(t)
	repo := gitRepo(t, "invalid-json-field")
	body, status, _ := doRequest(t, srv, "POST", "/api/v1/projects", `{"path":`+quote(repo)+`,"projectId":"invalid-json"}`)
	if status != http.StatusCreated {
		t.Fatalf("seed create = %d, want 201; body=%s", status, body)
	}

	tests := []struct {
		name    string
		body    string
		field   string
		reason  string
		message string
	}{
		{
			name:   "unknown coordinators key",
			body:   `{"displayName":"Invalid","config":{"coordinators":{"enabled":true,"foo":1}}}`,
			field:  "config.coordinators.foo",
			reason: "unknown field",
		},
		{
			name:   "unknown key in an agent profile",
			body:   `{"displayName":"Invalid","config":{"coordinators":{"agentProfiles":[{"id":"fast","bogus":true}]}}}`,
			field:  "config.coordinators.agentProfiles[0].bogus",
			reason: "unknown field",
		},
		{
			name:   "type mismatch on a limit",
			body:   `{"displayName":"Invalid","config":{"coordinators":{"maxWorkers":"9"}}}`,
			field:  "config.coordinators.maxWorkers",
			reason: "expected int, got string",
		},
		{
			name:   "top-level unknown key",
			body:   `{"displayName":"Invalid","config":{},"surprise":"!"}`,
			field:  "surprise",
			reason: "unknown field",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body, status, _ := doRequest(t, srv, "PUT", "/api/v1/projects/invalid-json", tt.body)
			assertErrorCode(t, body, status, http.StatusBadRequest, "INVALID_JSON")
			var got invalidJSONBody
			mustJSON(t, body, &got)
			if got.Details["field"] != tt.field || got.Details["reason"] != tt.reason {
				t.Fatalf("details = %#v, want field %q reason %q", got.Details, tt.field, tt.reason)
			}
		})
	}

	body, status, _ = doRequest(t, srv, "PUT", "/api/v1/projects/invalid-json", `{`)
	assertErrorCode(t, body, status, http.StatusBadRequest, "INVALID_JSON")
	var generic invalidJSONBody
	mustJSON(t, body, &generic)
	if generic.Message != "Invalid JSON body" || generic.Details != nil {
		t.Fatalf("malformed body = %#v, want generic message without details", generic)
	}
}

// TestProjectsAPI_CoordinatorsConfigRoundTrip PUTs back a config the daemon
// returned; every coordinators field must be accepted unchanged.
func TestProjectsAPI_CoordinatorsConfigRoundTrip(t *testing.T) {
	srv := newTestServer(t)
	repo := gitRepo(t, "coordinators-round-trip")
	body, status, _ := doRequest(t, srv, "POST", "/api/v1/projects", `{"path":`+quote(repo)+`,"projectId":"coord-rt"}`)
	if status != http.StatusCreated {
		t.Fatalf("seed create = %d, want 201; body=%s", status, body)
	}

	const settings = `{"displayName":"Coordinators","config":{
		"defaultBranch":"dev",
		"ciGate":{},
		"coordinators":{
			"enabled":true,
			"maxCoordinators":3,
			"maxWorkersPerCoordinator":3,
			"maxWorkers":9,
			"agentProfiles":[
				{"id":"fast","label":"Fast","harness":"claude-code","model":"sonnet","effort":"low","weight":2},
				{"id":"deep","harness":"codex","model":"gpt-5","effort":"high"}
			],
			"coordinatorAgent":{"mode":"fixed","profileId":"deep"},
			"workerAgent":{"mode":"random","profileIds":["fast","deep"]},
			"rootWorkerAgent":{"mode":"inherit"},
			"allowedHosts":["IMac","Linux","Z600"],
			"allowLocalWorkers":true
		}}}`
	body, status, _ = doRequest(t, srv, "PUT", "/api/v1/projects/coord-rt", settings)
	if status != http.StatusOK {
		t.Fatalf("PUT settings = %d, want 200; body=%s", status, body)
	}

	first := coordinatorsConfigOf(t, srv, "coord-rt")
	put, err := json.Marshal(map[string]any{"displayName": "Coordinators", "config": first})
	if err != nil {
		t.Fatal(err)
	}
	body, status, _ = doRequest(t, srv, "PUT", "/api/v1/projects/coord-rt", string(put))
	if status != http.StatusOK {
		t.Fatalf("round-trip PUT = %d, want 200; body=%s\nsent=%s", status, body, put)
	}
	second := coordinatorsConfigOf(t, srv, "coord-rt")
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("round trip changed the config\nfirst:  %#v\nsecond: %#v", first, second)
	}
}

// coordinatorsConfigOf reads the project config as the daemon emits it.
func coordinatorsConfigOf(t *testing.T, srv *httptest.Server, id string) map[string]any {
	t.Helper()
	body, status, _ := doRequest(t, srv, "GET", "/api/v1/projects/"+id, "")
	if status != http.StatusOK {
		t.Fatalf("GET project = %d, want 200; body=%s", status, body)
	}
	var got struct {
		Project struct {
			Config map[string]any `json:"config"`
		} `json:"project"`
	}
	mustJSON(t, body, &got)
	return got.Project.Config
}
