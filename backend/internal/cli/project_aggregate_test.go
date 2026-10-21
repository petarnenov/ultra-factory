package cli

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

const aggregateEffectiveBody = `{"status":"ok","project":{"id":"app"},"aggregateEffective":[` +
	`{"hostId":"h_imac","hostLabel":"IMac","projectId":"app","source":"discovered","matchedBy":"repo","stale":false},` +
	`{"hostId":"h_z600","hostLabel":"Z600","projectId":"app","source":"explicit","stale":true}]}`

func TestProjectAggregateList_Table(t *testing.T) {
	cfg := setConfigEnv(t)
	srv, capture := projectServer(t, http.StatusOK, aggregateEffectiveBody)
	writeRunFileFor(t, cfg, srv)

	out, errOut, err := executeCLI(t, Deps{ProcessAlive: func(int) bool { return true }}, "project", "aggregate", "ls", "app")
	if err != nil {
		t.Fatalf("unexpected error: %v\nstderr=%s", err, errOut)
	}
	if capture.method != http.MethodGet || capture.path != "/api/v1/projects/app" {
		t.Fatalf("request = %s %s, want GET /api/v1/projects/app", capture.method, capture.path)
	}
	for _, want := range []string{"HOST ID", "SOURCE", "MATCHED BY", "h_imac", "discovered", "repo", "h_z600", "explicit"} {
		if !strings.Contains(out, want) {
			t.Fatalf("output missing %q:\n%s", want, out)
		}
	}
}

func TestProjectAggregateList_JSON(t *testing.T) {
	cfg := setConfigEnv(t)
	srv, _ := projectServer(t, http.StatusOK, aggregateEffectiveBody)
	writeRunFileFor(t, cfg, srv)

	out, errOut, err := executeCLI(t, Deps{ProcessAlive: func(int) bool { return true }}, "project", "aggregate", "ls", "app", "--json")
	if err != nil {
		t.Fatalf("unexpected error: %v\nstderr=%s", err, errOut)
	}
	var got projectAggregateResult
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("decode json: %v\nout=%s", err, out)
	}
	if len(got.AggregateEffective) != 2 || got.AggregateEffective[0].Source != "discovered" || !got.AggregateEffective[1].Stale {
		t.Fatalf("aggregateEffective = %+v", got.AggregateEffective)
	}
}

func TestProjectAggregateList_Empty(t *testing.T) {
	cfg := setConfigEnv(t)
	srv, _ := projectServer(t, http.StatusOK, `{"status":"ok","project":{"id":"app"},"aggregateEffective":[]}`)
	writeRunFileFor(t, cfg, srv)

	out, _, err := executeCLI(t, Deps{ProcessAlive: func(int) bool { return true }}, "project", "aggregate", "ls", "app")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "No aggregate projects") {
		t.Fatalf("empty output = %q", out)
	}
}

func TestProjectAggregateRefresh_PostsAndPrintsSet(t *testing.T) {
	cfg := setConfigEnv(t)
	srv, capture := projectServer(t, http.StatusOK, `{"aggregateEffective":[{"hostId":"h_imac","hostLabel":"IMac","projectId":"app","source":"discovered","matchedBy":"repo","stale":false}]}`)
	writeRunFileFor(t, cfg, srv)

	out, errOut, err := executeCLI(t, Deps{ProcessAlive: func(int) bool { return true }}, "project", "aggregate", "refresh", "app")
	if err != nil {
		t.Fatalf("unexpected error: %v\nstderr=%s", err, errOut)
	}
	if capture.method != http.MethodPost || capture.path != "/api/v1/projects/app/aggregate/refresh" {
		t.Fatalf("request = %s %s, want POST refresh route", capture.method, capture.path)
	}
	if !strings.Contains(out, "h_imac") {
		t.Fatalf("refresh output missing host:\n%s", out)
	}
}

func TestProjectAggregate_MissingIDIsUsageError(t *testing.T) {
	setConfigEnv(t)
	for _, args := range [][]string{{"project", "aggregate", "ls"}, {"project", "aggregate", "refresh", " "}} {
		_, _, err := executeCLI(t, Deps{ProcessAlive: func(int) bool { return true }}, args...)
		if err == nil {
			t.Fatalf("%v: expected usage error", args)
		}
		if _, ok := err.(usageError); !ok {
			t.Fatalf("%v: error = %T %v, want usageError", args, err, err)
		}
	}
}
