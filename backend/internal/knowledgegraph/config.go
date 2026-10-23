// Package knowledgegraph connects the daemon to the central Neo4j knowledge
// graph (ADR 0012): task outcomes and lessons go in, prior context comes out.
package knowledgegraph

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Config locates the graph. A zero URL disables the integration.
type Config struct {
	URL      string `json:"url"`
	User     string `json:"user"`
	Password string `json:"password"`
	Database string `json:"database"`
	// Timeout bounds one query; spawn-time lookups never wait longer.
	Timeout time.Duration `json:"-"`
	// EmbeddingURL is an Ollama endpoint for semantic search (ADR 0013).
	// Empty keeps full-text search.
	EmbeddingURL   string `json:"embeddingUrl"`
	EmbeddingModel string `json:"embeddingModel"`
	// MinScore drops vector matches below it; Neo4j cosine scores are
	// (1 + cos) / 2, so 0.70 keeps cos >= 0.40.
	MinScore float64 `json:"minScore"`
	// MergeScore merges a new lesson into a stored one of the same kind
	// scoring at least this (ADR 0014); 0.94 keeps cos >= 0.88.
	MergeScore float64 `json:"mergeScore"`
}

// SemanticEnabled reports whether embeddings are configured.
func (c Config) SemanticEnabled() bool { return c.EmbeddingURL != "" }

// Enabled reports whether a graph is configured.
func (c Config) Enabled() bool { return c.URL != "" }

const (
	defaultUser     = "neo4j"
	defaultDatabase = "neo4j"
	defaultTimeout  = 3 * time.Second
	// DefaultEmbeddingModel has 1024 dimensions, matching schema.cypher;
	// it ranked best on AO lessons and also embeds non-English text.
	DefaultEmbeddingModel = "bge-m3"
	defaultMinScore       = 0.70
	defaultMergeScore     = 0.94
	// DefaultConfigFile sits next to the daemon's data dir, so ~/.ao/data
	// reads ~/.ao/knowledge-graph.json and a scratch data dir reads none.
	DefaultConfigFile = "knowledge-graph.json"
)

// Load reads DefaultConfigFile beside dataDir (or AO_KNOWLEDGE_GRAPH_CONFIG),
// then overlays AO_KNOWLEDGE_GRAPH_*. A missing file is not an error.
func Load(getenv func(string) string, dataDir string) (Config, error) {
	var cfg Config
	path := strings.TrimSpace(getenv("AO_KNOWLEDGE_GRAPH_CONFIG"))
	if path == "" {
		if dataDir != "" {
			path = filepath.Join(filepath.Dir(filepath.Clean(dataDir)), DefaultConfigFile)
		}
	}
	if path != "" {
		fileCfg, err := readConfigFile(path)
		if err != nil {
			return Config{}, err
		}
		cfg = fileCfg
	}
	if v := strings.TrimSpace(getenv("AO_KNOWLEDGE_GRAPH_URL")); v != "" {
		cfg.URL = v
	}
	if v := strings.TrimSpace(getenv("AO_KNOWLEDGE_GRAPH_USER")); v != "" {
		cfg.User = v
	}
	if v := getenv("AO_KNOWLEDGE_GRAPH_PASSWORD"); v != "" {
		cfg.Password = v
	}
	if v := strings.TrimSpace(getenv("AO_KNOWLEDGE_GRAPH_DATABASE")); v != "" {
		cfg.Database = v
	}
	if v := strings.TrimSpace(getenv("AO_KNOWLEDGE_GRAPH_EMBEDDING_URL")); v != "" {
		cfg.EmbeddingURL = v
	}
	if v := strings.TrimSpace(getenv("AO_KNOWLEDGE_GRAPH_EMBEDDING_MODEL")); v != "" {
		cfg.EmbeddingModel = v
	}
	if v := strings.TrimSpace(getenv("AO_KNOWLEDGE_GRAPH_MIN_SCORE")); v != "" {
		f, err := strconv.ParseFloat(v, 64)
		if err != nil || f < 0 || f > 1 {
			return Config{}, fmt.Errorf("invalid AO_KNOWLEDGE_GRAPH_MIN_SCORE %q", v)
		}
		cfg.MinScore = f
	}
	if v := strings.TrimSpace(getenv("AO_KNOWLEDGE_GRAPH_MERGE_SCORE")); v != "" {
		f, err := strconv.ParseFloat(v, 64)
		if err != nil || f < 0 || f > 1 {
			return Config{}, fmt.Errorf("invalid AO_KNOWLEDGE_GRAPH_MERGE_SCORE %q", v)
		}
		cfg.MergeScore = f
	}
	if v := strings.TrimSpace(getenv("AO_KNOWLEDGE_GRAPH_TIMEOUT")); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil || d <= 0 {
			return Config{}, fmt.Errorf("invalid AO_KNOWLEDGE_GRAPH_TIMEOUT %q", v)
		}
		cfg.Timeout = d
	}
	if !cfg.Enabled() {
		return Config{}, nil
	}
	u, err := url.Parse(cfg.URL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return Config{}, fmt.Errorf("knowledge graph url %q must be http(s)://host[:port]", cfg.URL)
	}
	cfg.URL = strings.TrimRight(cfg.URL, "/")
	if cfg.User == "" {
		cfg.User = defaultUser
	}
	if cfg.Database == "" {
		cfg.Database = defaultDatabase
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = defaultTimeout
	}
	if cfg.SemanticEnabled() {
		eu, err := url.Parse(cfg.EmbeddingURL)
		if err != nil || (eu.Scheme != "http" && eu.Scheme != "https") || eu.Host == "" {
			return Config{}, fmt.Errorf("knowledge graph embeddingUrl %q must be http(s)://host[:port]", cfg.EmbeddingURL)
		}
		cfg.EmbeddingURL = strings.TrimRight(cfg.EmbeddingURL, "/")
		if cfg.EmbeddingModel == "" {
			cfg.EmbeddingModel = DefaultEmbeddingModel
		}
		if cfg.MinScore == 0 {
			cfg.MinScore = defaultMinScore
		}
		if cfg.MergeScore == 0 {
			cfg.MergeScore = defaultMergeScore
		}
	}
	return cfg, nil
}

func readConfigFile(path string) (Config, error) {
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return Config{}, nil
	}
	if err != nil {
		return Config{}, fmt.Errorf("stat %s: %w", path, err)
	}
	// The file holds a password: refuse one others can read.
	if info.Mode().Perm()&0o077 != 0 {
		return Config{}, fmt.Errorf("%s must not be readable by group or others (chmod 600)", path)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read %s: %w", path, err)
	}
	var cfg Config
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return Config{}, fmt.Errorf("parse %s: %w", path, err)
	}
	return cfg, nil
}
