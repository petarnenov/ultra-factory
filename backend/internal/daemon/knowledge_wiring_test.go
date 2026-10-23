package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/cdc"
	"github.com/aoagents/agent-orchestrator/backend/internal/consultations"
	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/knowledgegraph"
)

// lockedBuffer is a log sink safe for the recorder goroutines.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

type knowledgeGraphFailureStore struct {
	checks   []domain.PullRequestCheck
	comments []domain.PullRequestComment
}

func (s knowledgeGraphFailureStore) ListChecks(context.Context, string) ([]domain.PullRequestCheck, error) {
	return s.checks, nil
}

func (s knowledgeGraphFailureStore) ListPRComments(context.Context, string) ([]domain.PullRequestComment, error) {
	return s.comments, nil
}

func TestKnowledgeGraphPRFailuresFiltersStoredEvidence(t *testing.T) {
	facts := knowledgeGraphPRFailures{store: knowledgeGraphFailureStore{
		checks: []domain.PullRequestCheck{
			{Name: "failed", Status: domain.PRCheckFailed, LogTail: "tail"},
			{Name: "passed", Status: domain.PRCheckPassed},
			{Name: "running", Status: domain.PRCheckInProgress},
		},
		comments: []domain.PullRequestComment{
			{Author: "human", File: "a.go", Body: "fix this"},
			{Author: "resolved", Body: "closed", Resolved: true},
			{Author: "bot", Body: "automated", IsBot: true},
		},
	}}
	got, err := facts.ForPR(context.Background(), "https://example/pr/1")
	if err != nil {
		t.Fatalf("ForPR() error = %v", err)
	}
	want := knowledgegraph.PRFailureEvidence{
		Checks:   []knowledgegraph.FailedCheck{{Name: "failed", LogTail: "tail"}},
		Comments: []knowledgegraph.UnresolvedComment{{Author: "human", File: "a.go", Body: "fix this"}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ForPR() = %#v, want %#v", got, want)
	}
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func writeKnowledgeConfig(t *testing.T, cfg map[string]any) string {
	t.Helper()
	raw, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	path := filepath.Join(t.TempDir(), "knowledge-graph.json")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

func TestStartKnowledgeGraphWithSemanticSearch(t *testing.T) {
	statements := make(chan string, 16)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The embed loop first probes the model's vector size.
		if r.URL.Path == "/api/embed" {
			_, _ = w.Write([]byte(`{"embeddings":[[0.1,0.2,0.3]]}`))
			return
		}
		var body struct {
			Statement string `json:"statement"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		select {
		case statements <- body.Statement:
		default:
		}
		_, _ = w.Write([]byte(`{"data":{"fields":[],"values":[]}}`))
	}))
	defer srv.Close()
	path := writeKnowledgeConfig(t, map[string]any{"url": srv.URL, "embeddingUrl": srv.URL + "/"})
	env := map[string]string{"AO_KNOWLEDGE_GRAPH_CONFIG": path, "AO_KNOWLEDGE_GRAPH_CONSULTATION_SCAN": "0"}
	logs := &lockedBuffer{}

	k, svc := startKnowledgeGraph(context.Background(), knowledgeGraphDeps{
		Getenv: func(key string) string { return env[key] },
		Events: cdc.NewBroadcaster(), HostID: "host1",
		Log: slog.New(slog.NewTextHandler(logs, nil)),
	})

	if k == nil || svc == nil {
		t.Fatalf("startKnowledgeGraph() = %v, %v; want started", k, svc)
	}
	select {
	case st := <-statements:
		if !strings.Contains(st, "embedding IS NULL") {
			t.Fatalf("first statement = %q, want embedding backfill", st)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("embed loop did not query the graph")
	}
	stopped := make(chan struct{})
	go func() {
		k.Stop()
		close(stopped)
	}()
	select {
	case <-stopped:
	case <-time.After(2 * time.Second):
		t.Fatalf("Stop() did not return")
	}
	if got := logs.String(); !strings.Contains(got, "semantic=true") || !strings.Contains(got, "embeddingModel=bge-m3") {
		t.Fatalf("logs = %q, want semantic enabled", got)
	}
}

func TestStartKnowledgeGraphRejectsBadEmbeddingURL(t *testing.T) {
	path := writeKnowledgeConfig(t, map[string]any{"url": "http://kg:7474", "embeddingUrl": "ftp://ollama"})
	env := map[string]string{"AO_KNOWLEDGE_GRAPH_CONFIG": path, "AO_KNOWLEDGE_GRAPH_CONSULTATION_SCAN": "0"}
	logs := &lockedBuffer{}

	k, svc := startKnowledgeGraph(context.Background(), knowledgeGraphDeps{
		Getenv: func(key string) string { return env[key] },
		Log:    slog.New(slog.NewTextHandler(logs, nil)),
	})

	if k != nil || svc != nil {
		t.Fatalf("startKnowledgeGraph() = %v, %v; want disabled", k, svc)
	}
	if !strings.Contains(logs.String(), "invalid configuration") {
		t.Fatalf("logs = %q, want invalid configuration warning", logs.String())
	}
}

func TestKnowledgeGraphScansCanonicalArchiveAndStops(t *testing.T) {
	stateRoot := t.TempDir()
	dir := filepath.Join(stateRoot, "architecture-consults/archive/2026-10-23/case")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	original := []byte(`{"requester":"Recorded engineer","archive":{"status":"verified"}}`)
	if err := os.WriteFile(filepath.Join(dir, "state.json"), original, 0600); err != nil {
		t.Fatal(err)
	}
	imported := make(chan string, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Statement  string         `json:"statement"`
			Parameters map[string]any `json:"parameters"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			return
		}
		if strings.Contains(body.Statement, "MERGE (c:ArchitectureConsultation") {
			if summary, ok := body.Parameters["summary"].(string); ok {
				select {
				case imported <- summary:
				default:
				}
			}
		}
		_, _ = w.Write([]byte(`{"data":{"fields":[],"values":[]}}`))
	}))
	defer srv.Close()
	path := writeKnowledgeConfig(t, map[string]any{"url": srv.URL})
	k, _ := startKnowledgeGraph(context.Background(), knowledgeGraphDeps{Getenv: func(key string) string {
		if key == "AO_KNOWLEDGE_GRAPH_CONFIG" {
			return path
		}
		return ""
	}, DataDir: filepath.Join(stateRoot, "data"), HostID: "host1", Events: cdc.NewBroadcaster(), Log: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if k == nil {
		t.Fatal("graph did not start")
	}
	defer k.Stop()
	select {
	case raw := <-imported:
		var summary consultations.Summary
		if err := json.Unmarshal([]byte(raw), &summary); err != nil {
			t.Fatal(err)
		}
		if summary.Requester != "Recorded engineer" || summary.Host != "host1" || summary.Archive != "verified" {
			t.Fatal("canonical archive was not wired")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("startup scanner did not import canonical archive")
	}
	after, err := os.ReadFile(filepath.Join(dir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(original, after) {
		t.Fatal("scanner modified the archive")
	}
}

func TestKnowledgeGraphScanOptOutAndUnavailableStatus(t *testing.T) {
	for _, disabled := range []bool{false, true} {
		t.Run(fmt.Sprintf("disabled=%t", disabled), func(t *testing.T) {
			var writes atomic.Int64
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				writes.Add(1)
				w.WriteHeader(http.StatusServiceUnavailable)
			}))
			defer srv.Close()
			path := writeKnowledgeConfig(t, map[string]any{"url": srv.URL})
			k, _ := startKnowledgeGraph(context.Background(), knowledgeGraphDeps{Getenv: func(key string) string {
				if key == "AO_KNOWLEDGE_GRAPH_CONFIG" {
					return path
				}
				if disabled && key == "AO_KNOWLEDGE_GRAPH_CONSULTATION_SCAN" {
					return "0"
				}
				return ""
			}, DataDir: filepath.Join(t.TempDir(), "data"), HostID: "host1", Events: cdc.NewBroadcaster(), Log: slog.New(slog.NewTextHandler(io.Discard, nil))})
			if k == nil {
				t.Fatal("graph did not start")
			}
			if !disabled {
				deadline := time.Now().Add(2 * time.Second)
				for k.Consultations.Status().LastScan == "" && time.Now().Before(deadline) {
					time.Sleep(time.Millisecond)
				}
			}
			k.Stop()
			status := k.Consultations.Status()
			if disabled && (status.Enabled || writes.Load() != 0) {
				t.Fatal("opt-out allowed graph requests")
			}
			if !disabled && (!status.Enabled || status.Error == "" || status.Imported != 0) {
				t.Fatal("unavailable graph hid scan failure")
			}
		})
	}
}
