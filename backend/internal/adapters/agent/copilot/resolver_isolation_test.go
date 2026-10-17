package copilot

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

func isolatedResolverHome(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("Unix candidate enumeration; Windows keeps its platform-specific resolver")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("PATH", t.TempDir())
	t.Setenv("VOLTA_HOME", filepath.Join(home, ".volta"))
	t.Setenv("FNM_DIR", filepath.Join(home, "fnm"))
	data := t.TempDir()
	if !filepath.IsAbs(data) {
		t.Fatal("XDG fixture root must be absolute")
	}
	t.Setenv("XDG_DATA_HOME", data)
	return home
}

func fixtureResolverOps(path string, executables map[string]bool, links map[string]string) copilotResolverOps {
	return copilotResolverOps{
		lookPath: func(name string) (string, error) {
			if name == "copilot" && path != "" {
				return path, nil
			}
			return "", exec.ErrNotFound
		},
		isExecutableFile: func(path string) bool { return executables[path] },
		evalSymlinks: func(path string) (string, error) {
			if target, ok := links[path]; ok {
				return target, nil
			}
			return "", os.ErrNotExist
		},
	}
}

func TestResolveCopilotBinaryFallbacks(t *testing.T) {
	home := isolatedResolverHome(t)
	// Enumeration remains real but every enumerated directory is a fixture.
	nvm := writeExecutable(t, filepath.Join(home, ".nvm", "versions", "node", "v22.23.1", "bin", "copilot"))
	npm := filepath.Join(home, ".npm-global", "bin", "copilot")
	loader := filepath.Join(home, "package", "npm-loader.js")
	native := filepath.Join(filepath.Dir(loader), "node_modules", ".bin", "copilot-"+runtime.GOOS+"-"+runtime.GOARCH)
	linuxbrew, snap := "/home/linuxbrew/.linuxbrew/bin/copilot", "/snap/bin/copilot"
	pathBinary := filepath.Join(home, "path", "copilot")
	for _, tc := range []struct {
		name    string
		path    string
		present []string
		links   map[string]string
		want    string
	}{
		{name: "npm global", present: []string{npm, nvm}, want: npm},
		{name: "nvm", present: []string{nvm}, want: nvm},
		{name: "snap before nvm", present: []string{snap, nvm}, want: snap},
		{name: "linuxbrew before snap and nvm", present: []string{linuxbrew, snap, nvm}, want: linuxbrew},
		{name: "fixed paths before package managers", present: []string{"/usr/local/bin/copilot", "/opt/homebrew/bin/copilot", linuxbrew, snap, nvm}, want: "/usr/local/bin/copilot"},
		{name: "PATH first", path: pathBinary, present: []string{"/usr/local/bin/copilot", snap, nvm}, want: pathBinary},
		{name: "npm loader resolves to native", present: []string{npm, native}, links: map[string]string{npm: loader}, want: native},
		{name: "PATH loader resolves to native", path: pathBinary, present: []string{native}, links: map[string]string{pathBinary: loader}, want: native},
		{name: "missing native keeps loader", present: []string{npm, nvm}, links: map[string]string{npm: loader}, want: npm},
		{name: "PATH missing native keeps loader", path: pathBinary, links: map[string]string{pathBinary: loader}, want: pathBinary},
		{name: "ordinary symlink", present: []string{snap}, links: map[string]string{snap: "/usr/bin/snap"}, want: snap},
		{name: "missing all candidates"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			present := make(map[string]bool)
			for _, candidate := range tc.present {
				present[candidate] = true
			}
			got, err := resolveCopilotBinary(context.Background(), fixtureResolverOps(tc.path, present, tc.links))
			if tc.want == "" {
				if got != "" || !errors.Is(err, ports.ErrAgentBinaryNotFound) || err.Error() != "copilot: "+ports.ErrAgentBinaryNotFound.Error() {
					t.Fatalf("resolve = (%q, %v), want wrapped not-found error", got, err)
				}
			} else if err != nil || got != tc.want {
				t.Fatalf("resolve = (%q, %v), want %q", got, err, tc.want)
			}
		})
	}
}

func TestCopilotResolverRetainsFixedCandidateOrder(t *testing.T) {
	home := isolatedResolverHome(t)
	nvm := writeExecutable(t, filepath.Join(home, ".nvm", "versions", "node", "v22.23.1", "bin", "copilot"))
	ops := fixtureResolverOps("", nil, nil)
	var checked []string
	ops.isExecutableFile = func(path string) bool { checked = append(checked, path); return false }
	_, err := resolveCopilotBinary(context.Background(), ops)
	if !errors.Is(err, ports.ErrAgentBinaryNotFound) {
		t.Fatal(err)
	}
	want := []string{
		"/usr/local/bin/copilot", "/opt/homebrew/bin/copilot",
		filepath.Join(home, ".npm-global/bin/copilot"), filepath.Join(home, ".npm/bin/copilot"),
		filepath.Join(home, ".local/bin/copilot"), filepath.Join(home, ".copilot/bin/copilot"),
		filepath.Join(home, "Library/Application Support/Code/User/globalStorage/github.copilot-chat/copilotCli/copilot"),
		"/home/linuxbrew/.linuxbrew/bin/copilot", "/snap/bin/copilot",
		filepath.Join(home, ".bun/bin/copilot"), filepath.Join(home, ".yarn/bin/copilot"),
		filepath.Join(home, ".config/yarn/global/node_modules/.bin/copilot"),
		filepath.Join(home, ".local/share/mise/shims/copilot"), filepath.Join(home, ".asdf/shims/copilot"), nvm,
	}
	if !reflect.DeepEqual(checked, want) {
		t.Fatalf("candidate order = %v, want %v", checked, want)
	}
}

func TestCopilotResolverCancellation(t *testing.T) {
	isolatedResolverHome(t)
	for _, stage := range []string{"before lookup", "during lookup", "during candidate check"} {
		t.Run(stage, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			ops := fixtureResolverOps("", nil, nil)
			calls := 0
			ops.lookPath = func(string) (string, error) {
				calls++
				if stage == "during lookup" {
					cancel()
				}
				return "", exec.ErrNotFound
			}
			ops.isExecutableFile = func(string) bool { calls++; cancel(); return false }
			if stage == "before lookup" {
				cancel()
			}
			got, err := resolveCopilotBinary(ctx, ops)
			if got != "" || !errors.Is(err, context.Canceled) {
				t.Fatalf("resolve = (%q, %v), want cancellation", got, err)
			}
			wantCalls := map[string]int{"before lookup": 0, "during lookup": 1, "during candidate check": 2}[stage]
			if calls != wantCalls {
				t.Fatalf("operations = %d, want %d", calls, wantCalls)
			}
		})
	}
}

func TestCopilotResolverOperationsDoNotLeakBetweenCalls(t *testing.T) {
	isolatedResolverHome(t)
	first := fixtureResolverOps("", map[string]bool{"/snap/bin/copilot": true}, nil)
	second := fixtureResolverOps("", nil, nil)
	for range 2 {
		if got, err := resolveCopilotBinary(context.Background(), first); err != nil || got != "/snap/bin/copilot" {
			t.Fatalf("first = (%q, %v)", got, err)
		}
		if got, err := resolveCopilotBinary(context.Background(), second); got != "" || !errors.Is(err, ports.ErrAgentBinaryNotFound) {
			t.Fatalf("second = (%q, %v)", got, err)
		}
	}
}

func TestResolveCopilotBinaryDefaultUsesRealPATH(t *testing.T) {
	home := isolatedResolverHome(t)
	binary := writeExecutable(t, filepath.Join(home, "path", "copilot"))
	t.Setenv("PATH", filepath.Dir(binary))
	got, err := ResolveCopilotBinary(context.Background())
	if err != nil || got != binary {
		t.Fatalf("default = (%q, %v), want PATH fixture %q", got, err, binary)
	}
}
