// Package testgraph gates live consultation tests on dedicated test input.
package testgraph

import (
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/aoagents/agent-orchestrator/backend/internal/knowledgegraph"
)

// ConfigEnv selects the dedicated disposable graph test file.
const ConfigEnv = "AO_CONSULTATIONS_TEST_GRAPH_CONFIG"

// IDEnv confirms the exact target ID declared in that file.
const IDEnv = "AO_CONSULTATIONS_TEST_GRAPH_ID"

// Purpose distinguishes test-only config from normal graph config.
const Purpose = "architecture-consultation-disposable-test"

var targetID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,95}$`)

type targetFile struct {
	Purpose string `json:"purpose"`
	ID      string `json:"id"`
	Graph   struct {
		URL      string `json:"url"`
		User     string `json:"user"`
		Password string `json:"password"`
		Database string `json:"database"`
	} `json:"graph"`
}

// Target identifies a declared disposable test graph, not an attested server.
type Target struct {
	knowledgegraph.Runner
	ID       string
	URL      string
	Database string
}

// Open validates dedicated test input before creating any graph client.
func Open(getenv func(string) string, connect func(knowledgegraph.Config) knowledgegraph.Runner) (*Target, error) {
	if getenv == nil || connect == nil {
		return nil, errors.New("disposable test graph requires an environment reader and connector")
	}
	path, id := getenv(ConfigEnv), getenv(IDEnv)
	if !filepath.IsAbs(path) || !targetID.MatchString(id) {
		return nil, errors.New("live tests require explicit disposable test config and target ID")
	}
	info, err := os.Lstat(path)
	if err != nil {
		return nil, errors.New("dedicated disposable test config is unavailable")
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 || info.Size() > 64<<10 {
		return nil, errors.New("disposable test config must be a regular private file of at most 64 KiB")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, errors.New("dedicated disposable test config cannot be read")
	}
	defer func() { _ = file.Close() }()
	var input targetFile
	decoder := json.NewDecoder(io.LimitReader(file, 64<<10))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&input) != nil || decoder.Decode(&struct{}{}) != io.EOF {
		return nil, errors.New("invalid dedicated disposable test config")
	}
	if input.Purpose != Purpose || input.ID != id {
		return nil, errors.New("disposable test config purpose or target ID does not match explicit opt-in")
	}
	if input.Graph.URL == "" || strings.TrimSpace(input.Graph.User) == "" || input.Graph.Password == "" || strings.TrimSpace(input.Graph.Database) == "" {
		return nil, errors.New("disposable test config requires an explicit endpoint, user, password and database")
	}
	endpoint, err := url.Parse(input.Graph.URL)
	if err != nil || endpoint.User != nil || endpoint.RawQuery != "" || endpoint.Fragment != "" {
		return nil, errors.New("invalid disposable test graph endpoint")
	}
	values := map[string]string{
		"AO_KNOWLEDGE_GRAPH_URL": input.Graph.URL, "AO_KNOWLEDGE_GRAPH_USER": input.Graph.User,
		"AO_KNOWLEDGE_GRAPH_PASSWORD": input.Graph.Password, "AO_KNOWLEDGE_GRAPH_DATABASE": input.Graph.Database,
		"AO_KNOWLEDGE_GRAPH_TIMEOUT": "10s",
	}
	// This private adapter cannot read process config or inherit a graph target.
	cfg, err := knowledgegraph.Load(func(key string) string { return values[key] }, "")
	if err != nil || !cfg.Enabled() {
		return nil, errors.New("invalid disposable test graph connection configuration")
	}
	runner := connect(cfg)
	if runner == nil {
		return nil, errors.New("disposable test graph connector returned no client")
	}
	return &Target{Runner: runner, ID: id, URL: strings.TrimRight(cfg.URL, "/"), Database: cfg.Database}, nil
}
