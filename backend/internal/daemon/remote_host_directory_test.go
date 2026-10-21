package daemon

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/config"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/controllers"
)

func TestPublishRemoteHostAddressUsesScopedDigestWithoutRedirects(t *testing.T) {
	hash := strings.Repeat("a", 64)
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		if r.URL.Path != "/api/cloud/v1/remote-hosts/h_test/address" || r.Header.Get("Authorization") != "Bearer "+hash {
			t.Errorf("unexpected request: %s %s", r.URL.Path, r.Header.Get("Authorization"))
		}
		var input struct {
			URL string `json:"url"`
		}
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil || input.URL != "https://new.example.com:443" {
			t.Errorf("unexpected address: %+v (%v)", input, err)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	if err := publishRemoteHostAddress(context.Background(), srv.Client(), srv.URL, "h_test", "https://new.example.com:443", hash); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("address was not published")
	}
}

// Without AO_CLOUD_REMOTE_HOST_SYNC the daemon never contacts the control
// plane for remote hosts, however the bridge is configured.
func TestRemoteHostPublisherIsOffByDefault(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("control plane contacted: %s %s", r.Method, r.URL.Path)
	}))
	defer srv.Close()
	cfg := config.Config{CloudControlPlaneURL: srv.URL}
	bs := &controllers.BridgeService{ConfigPath: filepath.Join(t.TempDir(), "mobile", "config.json"), HostID: "h_test"}
	if startRemoteHostAddressPublisher(context.Background(), cfg, bs, slog.New(slog.NewTextHandler(io.Discard, nil))) {
		t.Fatal("publisher started with cloud sync off")
	}
	cfg.CloudRemoteHostSync = true
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if !startRemoteHostAddressPublisher(ctx, cfg, bs, slog.New(slog.NewTextHandler(io.Discard, nil))) {
		t.Fatal("publisher did not start with cloud sync on")
	}
}
