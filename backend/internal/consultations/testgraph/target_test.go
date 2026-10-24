package testgraph

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/knowledgegraph"
)

type fakeRunner struct{ calls int }

func (r *fakeRunner) Run(context.Context, string, map[string]any) ([]knowledgegraph.Row, error) {
	r.calls++
	return nil, nil
}

func dedicatedConfig(t *testing.T) (string, map[string]string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "disposable-test-graph.json")
	writeConfig(t, path, map[string]any{
		"purpose": Purpose, "id": "fixture-639",
		"graph": map[string]string{"url": "https://isolated.example.test:17439/", "user": "fixture-user", "password": "private-sentinel", "database": "fixture-db"},
	})
	return path, map[string]string{ConfigEnv: path, IDEnv: "fixture-639"}
}

func writeConfig(t *testing.T, path string, input map[string]any) {
	t.Helper()
	raw, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestOpenSelectsOnlyExplicitDisposableConfig(t *testing.T) {
	path, env := dedicatedConfig(t)
	env["AO_KNOWLEDGE_GRAPH_CONFIG"] = filepath.Join(t.TempDir(), "shared-config.json")
	env["AO_KNOWLEDGE_GRAPH_URL"] = "https://shared.invalid"
	env["AO_KNOWLEDGE_GRAPH_USER"] = "shared-user"
	env["AO_KNOWLEDGE_GRAPH_PASSWORD"] = "shared-secret"
	env["AO_KNOWLEDGE_GRAPH_DATABASE"] = "shared-db"
	env["AO_KNOWLEDGE_GRAPH_TIMEOUT"] = "1h"
	requested := []string{}
	spy := &fakeRunner{}
	connections := 0
	target, err := Open(func(key string) string {
		requested = append(requested, key)
		return env[key]
	}, func(cfg knowledgegraph.Config) knowledgegraph.Runner {
		connections++
		if cfg.URL != "https://isolated.example.test:17439" || cfg.User != "fixture-user" || cfg.Password != "private-sentinel" || cfg.Database != "fixture-db" || cfg.Timeout != 10*time.Second {
			t.Fatal("connector received inherited or incomplete graph configuration")
		}
		return spy
	})
	if err != nil {
		t.Fatal(err)
	}
	if connections != 1 || target.ID != "fixture-639" || target.URL != "https://isolated.example.test:17439" || target.Database != "fixture-db" {
		t.Fatal("explicit target identity was not preserved")
	}
	if len(requested) != 2 || requested[0] != ConfigEnv || requested[1] != IDEnv || env[ConfigEnv] != path {
		t.Fatal("selection consulted ambient graph configuration")
	}
	if _, err := target.Run(context.Background(), "fixture operation", nil); err != nil || spy.calls != 1 {
		t.Fatal("validated target did not delegate to its supplied connector")
	}
}

func TestOpenRefusesInvalidOptInBeforeConnecting(t *testing.T) {
	for _, name := range []string{"missing config", "missing identity", "invalid identity", "relative path", "missing file", "directory", "symlink", "public file", "oversized file", "malformed JSON", "trailing JSON", "ordinary graph config", "wrong purpose", "wrong identity", "missing URL", "missing user", "missing password", "missing database", "blank user", "blank database", "invalid URL", "unsupported scheme", "URL credentials", "URL query", "URL fragment"} {
		t.Run(name, func(t *testing.T) {
			path, env := dedicatedConfig(t)
			input := map[string]any{}
			raw, err := os.ReadFile(path)
			if err != nil || json.Unmarshal(raw, &input) != nil {
				t.Fatal("could not prepare dedicated fixture")
			}
			graph := input["graph"].(map[string]any)
			switch name {
			case "missing config":
				delete(env, ConfigEnv)
			case "missing identity":
				delete(env, IDEnv)
			case "invalid identity":
				env[IDEnv] = "../../shared"
			case "relative path":
				env[ConfigEnv] = "disposable-test-graph.json"
			case "missing file":
				env[ConfigEnv] = filepath.Join(t.TempDir(), "absent.json")
			case "directory":
				env[ConfigEnv] = t.TempDir()
			case "symlink":
				env[ConfigEnv] = filepath.Join(t.TempDir(), "alias.json")
				if err := os.Symlink(path, env[ConfigEnv]); err != nil {
					t.Skip("symlinks unavailable on this host")
				}
			case "public file":
				if err := os.Chmod(path, 0o644); err != nil {
					t.Fatal(err)
				}
			case "oversized file":
				if err := os.WriteFile(path, []byte(strings.Repeat(" ", (64<<10)+1)), 0o600); err != nil {
					t.Fatal(err)
				}
			case "malformed JSON":
				if err := os.WriteFile(path, []byte("{"), 0o600); err != nil {
					t.Fatal(err)
				}
			case "trailing JSON":
				if err := os.WriteFile(path, append(raw, []byte(" {}")...), 0o600); err != nil {
					t.Fatal(err)
				}
			case "ordinary graph config":
				writeConfig(t, path, map[string]any{"url": "https://shared.invalid", "password": "private-sentinel"})
			case "wrong purpose":
				input["purpose"] = "ordinary-graph"
			case "wrong identity":
				input["id"] = "another-fixture"
			case "missing URL":
				graph["url"] = ""
			case "missing user":
				graph["user"] = ""
			case "missing password":
				graph["password"] = ""
			case "missing database":
				graph["database"] = ""
			case "blank user":
				graph["user"] = " "
			case "blank database":
				graph["database"] = " "
			case "invalid URL":
				graph["url"] = "http://%private-sentinel"
			case "unsupported scheme":
				graph["url"] = "file:///private-sentinel"
			case "URL credentials":
				graph["url"] = "https://user:private-sentinel@isolated.example.test"
			case "URL query":
				graph["url"] = "https://isolated.example.test?secret=private-sentinel"
			case "URL fragment":
				graph["url"] = "https://isolated.example.test#private-sentinel"
			}
			if strings.HasPrefix(name, "wrong ") || strings.HasPrefix(name, "missing ") || strings.HasPrefix(name, "blank ") || strings.HasPrefix(name, "URL ") || name == "invalid URL" || name == "unsupported scheme" {
				writeConfig(t, path, input)
			}
			connections := 0
			spy := &fakeRunner{}
			target, err := Open(func(key string) string { return env[key] }, func(knowledgegraph.Config) knowledgegraph.Runner {
				connections++
				return spy
			})
			if err == nil || target != nil || connections != 0 || spy.calls != 0 {
				t.Fatal("invalid opt-in reached the connector or graph operations")
			}
			if strings.Contains(err.Error(), "private-sentinel") {
				t.Fatal("configuration error exposed credentials")
			}
		})
	}
}

func TestOpenRejectsMissingCollaborators(t *testing.T) {
	if _, err := Open(nil, nil); err == nil {
		t.Fatal("missing collaborators accepted")
	}
	_, env := dedicatedConfig(t)
	if _, err := Open(func(key string) string { return env[key] }, func(knowledgegraph.Config) knowledgegraph.Runner { return nil }); err == nil {
		t.Fatal("missing graph client accepted")
	}
}
