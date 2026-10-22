package knowledgegraph

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"
)

type embedRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

// ollamaStub answers /api/embed with one vector per input and records
// every request it receives.
type ollamaStub struct {
	mu       sync.Mutex
	requests []embedRequest
	// meta holds "METHOD path content-type" per request.
	meta []string
}

func (o *ollamaStub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var req embedRequest
	_ = json.NewDecoder(r.Body).Decode(&req)
	o.mu.Lock()
	o.requests = append(o.requests, req)
	o.meta = append(o.meta, r.Method+" "+r.URL.Path+" "+r.Header.Get("Content-Type"))
	o.mu.Unlock()
	vecs := make([][]float64, len(req.Input))
	for i := range req.Input {
		vecs[i] = []float64{float64(i), 0.5}
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"embeddings": vecs})
}

func (o *ollamaStub) metaSnapshot() []string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]string(nil), o.meta...)
}

func (o *ollamaStub) snapshot() []embedRequest {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]embedRequest(nil), o.requests...)
}

func newStubEmbedder(t *testing.T, model string) (*OllamaEmbedder, *ollamaStub) {
	t.Helper()
	stub := &ollamaStub{}
	srv := httptest.NewServer(stub)
	t.Cleanup(srv.Close)
	cfg := Config{EmbeddingURL: srv.URL, EmbeddingModel: model}
	return NewOllamaEmbedder(cfg, srv.Client()), stub
}

func TestOllamaEmbedQuery(t *testing.T) {
	tests := []struct {
		name      string
		model     string
		wantInput string
	}{
		{name: "plain model has no prefix", model: "bge-m3", wantInput: "flaky test"},
		{name: "nomic model gets query prefix", model: "nomic-embed-text", wantInput: "search_query: flaky test"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e, stub := newStubEmbedder(t, tt.model)

			vec, err := e.EmbedQuery(context.Background(), "flaky test")

			if err != nil {
				t.Fatalf("EmbedQuery() error = %v", err)
			}
			if !reflect.DeepEqual(vec, []float64{0, 0.5}) {
				t.Fatalf("vec = %v", vec)
			}
			want := []embedRequest{{Model: tt.model, Input: []string{tt.wantInput}}}
			if got := stub.snapshot(); !reflect.DeepEqual(got, want) {
				t.Fatalf("requests = %#v, want %#v", got, want)
			}
			if got := stub.metaSnapshot(); !reflect.DeepEqual(got, []string{"POST /api/embed application/json"}) {
				t.Fatalf("request meta = %q", got)
			}
		})
	}
}

func TestOllamaEmbedDocuments(t *testing.T) {
	tests := []struct {
		name      string
		model     string
		wantInput []string
	}{
		{name: "plain model has no prefix", model: "bge-m3", wantInput: []string{"a", "b"}},
		{
			name: "nomic model gets document prefix", model: "nomic-embed-text:v1.5",
			wantInput: []string{"search_document: a", "search_document: b"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e, stub := newStubEmbedder(t, tt.model)

			vecs, err := e.EmbedDocuments(context.Background(), []string{"a", "b"})

			if err != nil {
				t.Fatalf("EmbedDocuments() error = %v", err)
			}
			if !reflect.DeepEqual(vecs, [][]float64{{0, 0.5}, {1, 0.5}}) {
				t.Fatalf("vecs = %v", vecs)
			}
			if got := stub.snapshot()[0].Input; !reflect.DeepEqual(got, tt.wantInput) {
				t.Fatalf("input = %#v, want %#v", got, tt.wantInput)
			}
		})
	}
}

func TestOllamaClipsLongInputs(t *testing.T) {
	e, stub := newStubEmbedder(t, "nomic-embed-text")
	long := strings.Repeat("ж", maxEmbedText+10)

	if _, err := e.EmbedDocuments(context.Background(), []string{long}); err != nil {
		t.Fatalf("EmbedDocuments() error = %v", err)
	}

	got := stub.snapshot()[0].Input[0]
	want := "search_document: " + strings.Repeat("ж", maxEmbedText)
	if got != want {
		t.Fatalf("input has %d runes, want %d", utf8.RuneCountInString(got), utf8.RuneCountInString(want))
	}
}

func TestOllamaEmptyInputSkipsRequest(t *testing.T) {
	e, stub := newStubEmbedder(t, "bge-m3")

	vecs, err := e.EmbedDocuments(context.Background(), nil)

	if err != nil || vecs != nil {
		t.Fatalf("EmbedDocuments(nil) = %v, %v; want nil, nil", vecs, err)
	}
	if n := len(stub.snapshot()); n != 0 {
		t.Fatalf("requests = %d, want 0", n)
	}
}

func TestOllamaEmbedErrors(t *testing.T) {
	tests := []struct {
		name    string
		status  int
		body    string
		wantErr string
	}{
		{name: "error field", status: http.StatusNotFound, body: `{"error":"model not found"}`, wantErr: "embeddings: model not found"},
		{name: "non-2xx without error", status: http.StatusBadGateway, body: `{}`, wantErr: "embeddings: HTTP 502"},
		{name: "undecodable body", status: http.StatusOK, body: `<html>`, wantErr: "HTTP 200: undecodable"},
		{name: "count mismatch", status: http.StatusOK, body: `{"embeddings":[[1]]}`, wantErr: "count does not match"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer srv.Close()
			e := NewOllamaEmbedder(Config{EmbeddingURL: srv.URL, EmbeddingModel: "bge-m3"}, srv.Client())

			_, err := e.EmbedDocuments(context.Background(), []string{"a", "b"})

			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("EmbedDocuments() error = %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestOllamaEmbedQueryReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	e := NewOllamaEmbedder(Config{EmbeddingURL: srv.URL, EmbeddingModel: "bge-m3"}, srv.Client())

	vec, err := e.EmbedQuery(context.Background(), "x")

	if err == nil || vec != nil {
		t.Fatalf("EmbedQuery() = %v, %v; want error", vec, err)
	}
}

func TestOllamaTransportError(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close()
	e := NewOllamaEmbedder(Config{EmbeddingURL: url, EmbeddingModel: "bge-m3"}, nil)

	_, err := e.EmbedQuery(context.Background(), "x")

	if err == nil || !strings.HasPrefix(err.Error(), "embeddings: ") {
		t.Fatalf("EmbedQuery() error = %v, want wrapped transport error", err)
	}
}

func TestOllamaInvalidURL(t *testing.T) {
	e := NewOllamaEmbedder(Config{EmbeddingURL: "http://bad\x7f", EmbeddingModel: "bge-m3"}, nil)

	_, err := e.EmbedQuery(context.Background(), "x")

	if err == nil {
		t.Fatalf("EmbedQuery() error = nil, want request build error")
	}
}

func TestNewOllamaEmbedderDefaultClientUsesTimeout(t *testing.T) {
	e := NewOllamaEmbedder(Config{EmbeddingURL: "http://o:11434", EmbeddingModel: "m", Timeout: 750 * time.Millisecond}, nil)

	if e.http.Timeout != 750*time.Millisecond || e.url != "http://o:11434" || e.model != "m" {
		t.Fatalf("embedder = %+v, timeout %v", e, e.http.Timeout)
	}
}

func TestClipRunes(t *testing.T) {
	tests := []struct {
		name string
		in   string
		n    int
		want string
	}{
		{name: "shorter kept", in: "abc", n: 5, want: "abc"},
		{name: "exact kept", in: "abc", n: 3, want: "abc"},
		{name: "multibyte clipped by rune", in: "жжжж", n: 2, want: "жж"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := clipRunes(tt.in, tt.n); got != tt.want {
				t.Fatalf("clipRunes(%q, %d) = %q, want %q", tt.in, tt.n, got, tt.want)
			}
		})
	}
}
