package cli

import (
	"context"

	"github.com/spf13/cobra"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCLI_RemoteTargetCoveragePureHelpers(t *testing.T) {
	t.Run("targetsRemote", func(t *testing.T) {
		c := &commandContext{}
		if c.targetsRemote() {
			t.Fatal("empty selector targets remote")
		}
		c.hostSelector = "   "
		if c.targetsRemote() {
			t.Fatal("whitespace-only selector targets remote")
		}
		c.hostSelector = "build"
		if !c.targetsRemote() {
			t.Fatal("non-empty selector does not target remote")
		}
	})

	t.Run("localContext", func(t *testing.T) {
		c := &commandContext{hostSelector: "build", deps: Deps{ProcessAlive: func(int) bool { return true }}}
		lc := c.localContext()
		if lc.hostSelector != "" {
			t.Fatalf("localContext retained hostSelector %q", lc.hostSelector)
		}
		if lc.deps.ProcessAlive == nil {
			t.Fatal("localContext lost deps")
		}
	})

	t.Run("shellQuote", func(t *testing.T) {
		if got := shellQuote("safe.name-1"); got != "safe.name-1" {
			t.Fatalf("shellQuote(safe) = %q", got)
		}
		if got := shellQuote(""); got != "''" {
			t.Fatalf("shellQuote(empty) = %q, want ''", got)
		}
		if got := shellQuote("has space"); got != "'has space'" {
			t.Fatalf("shellQuote(space) = %q", got)
		}
		if got := shellQuote("it's"); got != `'it'"'"'s'` {
			t.Fatalf("shellQuote(quote) = %q", got)
		}
	})

	t.Run("normalizeRemoteURL", func(t *testing.T) {
		if got := normalizeRemoteURL("build.example:3001"); got != "http://build.example:3001" {
			t.Fatalf("normalizeRemoteURL = %q", got)
		}
		if got := normalizeRemoteURL("HTTPS://Example.com/"); got != "https://example.com" {
			t.Fatalf("normalizeRemoteURL = %q", got)
		}
	})

	t.Run("remoteBaseURL", func(t *testing.T) {
		base, err := remoteBaseURL("https://build.example/path")
		if err != nil || base != "https://build.example" {
			t.Fatalf("remoteBaseURL = %q, %v", base, err)
		}
		if _, err := remoteBaseURL("ftp://build.example"); err == nil {
			t.Fatal("remoteBaseURL accepted ftp")
		}
		if _, err := remoteBaseURL("https://"); err == nil {
			t.Fatal("remoteBaseURL accepted empty host")
		}
	})

	t.Run("remoteLabels", func(t *testing.T) {
		got := remoteLabels([]remoteEntry{{Label: "b"}, {Label: "a"}})
		if got != "a, b" {
			t.Fatalf("remoteLabels = %q, want %q", got, "a, b")
		}
	})
}

func TestCLI_RemoteTargetCoverageResolveCached(t *testing.T) {
	c := &commandContext{remote: &remoteTarget{label: "cached", baseURL: "https://cached.example", hostID: "host-cached", password: "pw"}}
	got, err := c.resolveRemoteTarget(context.Background())
	if err != nil || got != c.remote {
		t.Fatalf("cached resolve = %v, %v", got, err)
	}
}

func TestCLI_RemoteTargetCoverageResolveReadEntriesError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "remotes.json")
	if err := os.WriteFile(path, []byte("{not json}"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AO_REMOTES_FILE", path)
	c := &commandContext{hostSelector: "build"}
	if _, err := c.resolveRemoteTarget(context.Background()); err == nil || !strings.Contains(err.Error(), "read ") {
		t.Fatalf("resolveRemoteTarget() err = %v, want JSON read error", err)
	}
}

func TestCLI_RemoteTargetCoverageResolveInvalidBaseURL(t *testing.T) {
	path := writeRemotesFile(t, remoteEntry{Label: "build", URL: "https://", HostID: "host-build", Password: "pw"})
	_ = path
	c := &commandContext{hostSelector: "build"}
	if _, err := c.resolveRemoteTarget(context.Background()); err == nil || !strings.Contains(err.Error(), "remote host must use") {
		t.Fatalf("resolveRemoteTarget() err = %v, want base URL error", err)
	}
}

func TestCLI_RemoteTargetCoverageResolveIdentityError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	urlStr := srv.URL
	srv.Close()
	writeRemotesFile(t, remoteEntry{Label: "build", URL: urlStr, HostID: "host-build", Password: "pw"})
	c := &commandContext{hostSelector: "build", deps: Deps{HTTPClient: &http.Client{}}}
	if _, err := c.resolveRemoteTarget(context.Background()); err == nil || !strings.Contains(err.Error(), "unreachable") {
		t.Fatalf("resolveRemoteTarget() err = %v, want identity probe error", err)
	}
}

func TestCLI_RemoteTargetCoverageRemoteSenderLabel(t *testing.T) {
	got := remoteSenderLabel("worker-1")
	if !strings.HasPrefix(got, "worker-1") {
		t.Fatalf("remoteSenderLabel = %q, want worker-1 prefix", got)
	}
}

func TestCLI_RemoteTargetCoverageAddHostFlag(t *testing.T) {
	ctx := &commandContext{}
	cmd := &cobra.Command{Use: "x"}
	addHostFlag(cmd, ctx)
	if err := cmd.Flags().Parse([]string{"--host", "build"}); err != nil {
		t.Fatalf("parse host flag: %v", err)
	}
	if ctx.hostSelector != "build" {
		t.Fatalf("hostSelector = %q, want build", ctx.hostSelector)
	}
}

func TestCLI_RemoteTargetCoverageResolveSelectError(t *testing.T) {
	writeRemotesFile(t,
		remoteEntry{Label: "Twin", URL: "http://10.0.0.7:3001", HostID: "host-a", Password: "pw"},
		remoteEntry{Label: "twin", URL: "http://10.0.0.8:3001", HostID: "host-b", Password: "pw"},
	)
	c := &commandContext{hostSelector: "twin"}
	if _, err := c.resolveRemoteTarget(context.Background()); err == nil || !strings.Contains(err.Error(), "matches 2 saved hosts") || ExitCode(err) != 2 {
		t.Fatalf("err = %v, want ambiguous match usage error", err)
	}
}
