package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/cdc"
)

// lockedBuffer is a log sink safe for the recorder goroutines.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
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
	env := map[string]string{"AO_KNOWLEDGE_GRAPH_CONFIG": path}
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
	env := map[string]string{"AO_KNOWLEDGE_GRAPH_CONFIG": path}
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
