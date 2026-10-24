//go:build kglive && consultarchive_live

package consultations_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/aoagents/agent-orchestrator/backend/internal/consultations"
	"github.com/aoagents/agent-orchestrator/backend/internal/consultations/testgraph"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/controllers"
	"github.com/aoagents/agent-orchestrator/backend/internal/knowledgegraph"
)

// TestRealArchiveGraphAPI checks canonical evidence through the real graph/API.
func TestRealArchiveGraphAPI(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	client, err := testgraph.Open(os.Getenv, func(cfg knowledgegraph.Config) knowledgegraph.Runner {
		return knowledgegraph.NewClient(cfg, nil)
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("Declared disposable graph %s: %s/db/%s; schema and fixture writes enabled", client.ID, client.URL, client.Database)
	root, err := os.OpenRoot(filepath.Join(home, ".ao/architecture-consults/archive"))
	if os.IsNotExist(err) {
		t.Skip("real archive unavailable")
	}
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = root.Close() }()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	host := "real-archive-api-test-" + time.Now().UTC().Format("20060102T150405.000000000")
	d, err := consultations.ReadArchive(ctx, root, host, "2026-10-23", "ultra-factory-issue-639-d418fb8-20261023T211546Z", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	store := &consultations.Store{Write: client, Read: client}
	if err := store.Schema(ctx); err != nil {
		t.Fatal(err)
	}
	key, _ := consultations.DecodeID(d.ID)
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, err := client.Run(cleanup, `MATCH (c:ArchitectureConsultation {key:$key}) OPTIONAL MATCH (c)-[:HAS_ARTIFACT]->(a) DETACH DELETE c,a`, map[string]any{"key": key})
		if err != nil {
			t.Error(err)
		}
	})
	for range 2 {
		if err := store.Import(ctx, d); err != nil {
			t.Fatal(err)
		}
	}
	router := chi.NewRouter()
	controller := &controllers.ConsultationController{Svc: store}
	router.Route("/api/v1", controller.Register)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/knowledge/consultations/"+d.ID, nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != 200 {
		t.Fatalf("detail status %d", response.Code)
	}
	var got consultations.Detail
	if err := json.Unmarshal(response.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Requester != "" || got.Mode != "fallback_arbitration" || got.Rounds != 0 || got.ProposalVersions != 1 || got.Archive != "verified" || len(got.ConsulClosure) != 4 {
		t.Fatalf("incorrect API metadata %+v", got.Summary)
	}
	found := false
	for _, a := range got.Artifacts {
		if a.Phase == "ballot" {
			t.Fatal("arbitration manufactured a council ballot round")
		}
		if a.Name == "fallback-arbitration/fable.md" {
			found = a.Phase == "arbitration" && a.Hash != "" && a.Content != "" && len(a.Ballots) == 0
		}
	}
	if !found {
		t.Fatal("original fallback evidence missing from graph API")
	}
}
