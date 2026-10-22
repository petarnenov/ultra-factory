package controllers_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/config"
	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/controllers"
	aggregatesvc "github.com/aoagents/agent-orchestrator/backend/internal/service/aggregate"
	projectsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/project"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/sqlitetest"
)

// fakeAggregate returns one fixed row and counts refresh passes.
type fakeAggregate struct {
	refreshes int
	repos     []string
}

func (f *fakeAggregate) Effective(_ domain.ProjectID, repo string, _ domain.ProjectConfig) []aggregatesvc.Entry {
	f.repos = append(f.repos, repo)
	return []aggregatesvc.Entry{{HostID: "h_a", HostLabel: "Alpha", ProjectID: "app", Source: aggregatesvc.SourceDiscovered, MatchedBy: aggregatesvc.MatchedByRepo}}
}

func (f *fakeAggregate) Refresh(context.Context) { f.refreshes++ }

func newAggregateTestServer(t *testing.T, agg controllers.AggregateService) *httptest.Server {
	t.Helper()
	t.Setenv("GIT_CEILING_DIRECTORIES", os.TempDir())
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	store, err := sqlitetest.Open(t.TempDir())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	srv := httptest.NewServer(httpd.NewRouterWithControl(config.Config{}, log, nil, httpd.APIDeps{
		Projects:  projectsvc.New(store),
		Aggregate: agg,
	}, httpd.ControlDeps{}))
	t.Cleanup(srv.Close)
	return srv
}

func TestProjectsAPI_AggregateEffectiveOnProjectRead(t *testing.T) {
	agg := &fakeAggregate{}
	srv := newAggregateTestServer(t, agg)
	repo := gitRepo(t, "app")
	if _, status, _ := doRequest(t, srv, "POST", "/api/v1/projects", `{"path":`+quote(repo)+`,"projectId":"app","name":"App"}`); status != http.StatusCreated {
		t.Fatalf("POST project = %d", status)
	}
	body, status, _ := doRequest(t, srv, "GET", "/api/v1/projects/app", "")
	if status != http.StatusOK {
		t.Fatalf("GET project = %d; body=%s", status, body)
	}
	var get struct {
		AggregateEffective []aggregatesvc.Entry `json:"aggregateEffective"`
	}
	mustJSON(t, body, &get)
	if len(get.AggregateEffective) != 1 || get.AggregateEffective[0].HostID != "h_a" || get.AggregateEffective[0].Source != aggregatesvc.SourceDiscovered {
		t.Fatalf("aggregateEffective = %+v", get.AggregateEffective)
	}
	if len(agg.repos) != 1 {
		t.Fatalf("resolver calls = %d, want 1", len(agg.repos))
	}
}

func TestProjectsAPI_AggregateRefreshRunsPassAndReturnsSet(t *testing.T) {
	agg := &fakeAggregate{}
	srv := newAggregateTestServer(t, agg)
	repo := gitRepo(t, "app")
	if _, status, _ := doRequest(t, srv, "POST", "/api/v1/projects", `{"path":`+quote(repo)+`,"projectId":"app","name":"App"}`); status != http.StatusCreated {
		t.Fatalf("POST project = %d", status)
	}
	body, status, _ := doRequest(t, srv, "POST", "/api/v1/projects/app/aggregate/refresh", "")
	if status != http.StatusOK {
		t.Fatalf("refresh = %d; body=%s", status, body)
	}
	var res controllers.AggregateEffectiveResponse
	mustJSON(t, body, &res)
	if agg.refreshes != 1 || len(res.AggregateEffective) != 1 {
		t.Fatalf("refreshes = %d, rows = %+v", agg.refreshes, res.AggregateEffective)
	}
}

func TestProjectsAPI_AggregateRefreshUnknownProjectIs404(t *testing.T) {
	agg := &fakeAggregate{}
	srv := newAggregateTestServer(t, agg)
	body, status, _ := doRequest(t, srv, "POST", "/api/v1/projects/missing/aggregate/refresh", "")
	if status != http.StatusNotFound {
		t.Fatalf("refresh unknown = %d; body=%s", status, body)
	}
	if agg.refreshes != 0 {
		t.Fatalf("refresh ran for unknown project")
	}
}
