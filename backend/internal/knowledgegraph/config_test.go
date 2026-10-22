package knowledgegraph

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeConfig(t *testing.T, body string, perm os.FileMode) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "knowledge-graph.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if err := os.Chmod(path, perm); err != nil {
		t.Fatalf("chmod config: %v", err)
	}
	return path
}

func envFunc(env map[string]string) func(string) string {
	return func(k string) string { return env[k] }
}

func TestLoad(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "absent.json")
	full := `{"url":"https://kg.example:7473/","user":"ao","password":"pw","database":"graph"}`
	tests := []struct {
		name    string
		file    string
		perm    os.FileMode
		env     map[string]string
		want    Config
		wantErr string
	}{
		{name: "missing file disables", env: map[string]string{"AO_KNOWLEDGE_GRAPH_CONFIG": missing}},
		{name: "no config path and no data dir", env: map[string]string{}},
		{
			name: "private file with defaults", file: `{"url":"http://kg:7474"}`, perm: 0o600,
			want: Config{URL: "http://kg:7474", User: "neo4j", Database: "neo4j", Timeout: 3 * time.Second},
		},
		{
			name: "file values and trailing slash trimmed", file: full, perm: 0o600,
			want: Config{URL: "https://kg.example:7473", User: "ao", Password: "pw", Database: "graph", Timeout: 3 * time.Second},
		},
		{name: "group readable file refused", file: full, perm: 0o644, wantErr: "chmod 600"},
		{name: "undecodable file", file: `{`, perm: 0o600, wantErr: "parse"},
		{
			name: "env overlays file", file: full, perm: 0o600,
			env: map[string]string{
				"AO_KNOWLEDGE_GRAPH_URL":      " http://other:7474/ ",
				"AO_KNOWLEDGE_GRAPH_USER":     "envuser",
				"AO_KNOWLEDGE_GRAPH_PASSWORD": "envpw",
				"AO_KNOWLEDGE_GRAPH_DATABASE": "envdb",
				"AO_KNOWLEDGE_GRAPH_TIMEOUT":  "750ms",
			},
			want: Config{URL: "http://other:7474", User: "envuser", Password: "envpw", Database: "envdb", Timeout: 750 * time.Millisecond},
		},
		{
			name: "env only without file",
			env:  map[string]string{"AO_KNOWLEDGE_GRAPH_CONFIG": missing, "AO_KNOWLEDGE_GRAPH_URL": "http://kg:7474"},
			want: Config{URL: "http://kg:7474", User: "neo4j", Database: "neo4j", Timeout: 3 * time.Second},
		},
		{
			name:    "invalid url scheme",
			env:     map[string]string{"AO_KNOWLEDGE_GRAPH_CONFIG": missing, "AO_KNOWLEDGE_GRAPH_URL": "bolt://kg:7687"},
			wantErr: "must be http(s)",
		},
		{
			name:    "url without host",
			env:     map[string]string{"AO_KNOWLEDGE_GRAPH_CONFIG": missing, "AO_KNOWLEDGE_GRAPH_URL": "http://"},
			wantErr: "must be http(s)",
		},
		{
			name:    "unparsable timeout",
			env:     map[string]string{"AO_KNOWLEDGE_GRAPH_CONFIG": missing, "AO_KNOWLEDGE_GRAPH_TIMEOUT": "soon"},
			wantErr: "invalid AO_KNOWLEDGE_GRAPH_TIMEOUT",
		},
		{
			name:    "non-positive timeout",
			env:     map[string]string{"AO_KNOWLEDGE_GRAPH_CONFIG": missing, "AO_KNOWLEDGE_GRAPH_TIMEOUT": "-1s"},
			wantErr: "invalid AO_KNOWLEDGE_GRAPH_TIMEOUT",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := map[string]string{}
			for k, v := range tt.env {
				env[k] = v
			}
			if tt.file != "" {
				env["AO_KNOWLEDGE_GRAPH_CONFIG"] = writeConfig(t, tt.file, tt.perm)
			}
			got, err := Load(envFunc(env), "")
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("Load() error = %v, want containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Load() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("Load() = %+v, want %+v", got, tt.want)
			}
			if got.Enabled() != (tt.want.URL != "") {
				t.Fatalf("Enabled() = %v", got.Enabled())
			}
		})
	}
}

func TestLoadReadsDefaultFileBesideDataDir(t *testing.T) {
	aoDir := t.TempDir()
	path := filepath.Join(aoDir, DefaultConfigFile)
	if err := os.WriteFile(path, []byte(`{"url":"http://kg:7474"}`), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	got, err := Load(envFunc(nil), filepath.Join(aoDir, "data"))

	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got.URL != "http://kg:7474" {
		t.Fatalf("URL = %q, want file value", got.URL)
	}
}

// A scratch data dir (tests, isolated dev) must never pick up the user's
// ~/.ao config just because HOME points at the real home.
func TestLoadIgnoresHomeConfigForScratchDataDir(t *testing.T) {
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".ao"), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	cfgPath := filepath.Join(home, ".ao", DefaultConfigFile)
	if err := os.WriteFile(cfgPath, []byte(`{"url":"http://kg:7474"}`), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	scratch := filepath.Join(t.TempDir(), "data")

	got, err := Load(envFunc(map[string]string{"HOME": home}), scratch)

	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got.Enabled() {
		t.Fatalf("Load() = %+v, want disabled for a scratch data dir", got)
	}
}
