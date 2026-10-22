package knowledgegraph

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"unicode/utf8"
)

// Embedder turns text into vectors for semantic search.
type Embedder interface {
	// EmbedQuery embeds a search query; EmbedDocuments embeds stored text.
	EmbedQuery(ctx context.Context, text string) ([]float64, error)
	EmbedDocuments(ctx context.Context, texts []string) ([][]float64, error)
}

// OllamaEmbedder calls Ollama's /api/embed.
type OllamaEmbedder struct {
	url   string
	model string
	http  *http.Client
}

// NewOllamaEmbedder returns an embedder for cfg. hc nil bounds each call
// by cfg.Timeout.
func NewOllamaEmbedder(cfg Config, hc *http.Client) *OllamaEmbedder {
	if hc == nil {
		hc = &http.Client{Timeout: cfg.Timeout}
	}
	return &OllamaEmbedder{url: cfg.EmbeddingURL, model: cfg.EmbeddingModel, http: hc}
}

// maxEmbedText keeps one input far inside the model's context window.
const maxEmbedText = 4000

// nomic-embed models need task prefixes on every input.
func (e *OllamaEmbedder) prefix(query bool) string {
	if !strings.HasPrefix(e.model, "nomic-embed") {
		return ""
	}
	if query {
		return "search_query: "
	}
	return "search_document: "
}

// EmbedQuery embeds one query.
func (e *OllamaEmbedder) EmbedQuery(ctx context.Context, text string) ([]float64, error) {
	out, err := e.embed(ctx, []string{e.prefix(true) + clipRunes(text, maxEmbedText)})
	if err != nil {
		return nil, err
	}
	return out[0], nil
}

// EmbedDocuments embeds stored texts in one call.
func (e *OllamaEmbedder) EmbedDocuments(ctx context.Context, texts []string) ([][]float64, error) {
	inputs := make([]string, len(texts))
	for i, t := range texts {
		inputs[i] = e.prefix(false) + clipRunes(t, maxEmbedText)
	}
	return e.embed(ctx, inputs)
}

func (e *OllamaEmbedder) embed(ctx context.Context, inputs []string) ([][]float64, error) {
	if len(inputs) == 0 {
		return nil, nil
	}
	body, err := json.Marshal(map[string]any{"model": e.model, "input": inputs})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.url+"/api/embed", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := e.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("embeddings: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return nil, fmt.Errorf("embeddings: read response: %w", err)
	}
	var out struct {
		Embeddings [][]float64 `json:"embeddings"`
		Error      string      `json:"error"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("embeddings: HTTP %d: undecodable response", resp.StatusCode)
	}
	if out.Error != "" {
		return nil, fmt.Errorf("embeddings: %s", out.Error)
	}
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("embeddings: HTTP %d", resp.StatusCode)
	}
	if len(out.Embeddings) != len(inputs) {
		return nil, errors.New("embeddings: response count does not match inputs")
	}
	return out.Embeddings, nil
}

func clipRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n])
}
