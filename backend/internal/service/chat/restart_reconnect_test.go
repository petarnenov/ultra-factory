package chat_test

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/adapters/chatdriver/acp"
	"github.com/aoagents/agent-orchestrator/backend/internal/adapters/chatdriver/persistenthost"
	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	chatsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/chat"
)

// TestRestartProviderHelper is a minimal ACP agent whose prompts finish only
// once the release file exists.
func TestRestartProviderHelper(t *testing.T) {
	if os.Getenv("AO_TEST_CHAT_RESTART_PROVIDER") != "1" {
		return
	}
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		var request struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		if json.Unmarshal(scanner.Bytes(), &request) != nil {
			continue
		}
		switch request.Method {
		case "initialize":
			_, _ = fmt.Fprintf(os.Stdout, `{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":1,"agentCapabilities":{"sessionCapabilities":{"resume":{}}},"authMethods":[]}}`+"\n", request.ID)
		case "session/new":
			_, _ = fmt.Fprintf(os.Stdout, `{"jsonrpc":"2.0","id":%s,"result":{"sessionId":"restart-provider"}}`+"\n", request.ID)
		case "session/prompt":
			for {
				if _, err := os.Stat(os.Getenv("AO_TEST_CHAT_RESTART_RELEASE")); err == nil {
					break
				}
				time.Sleep(5 * time.Millisecond)
			}
			_ = os.Remove(os.Getenv("AO_TEST_CHAT_RESTART_RELEASE"))
			_, _ = fmt.Fprintln(os.Stdout, `{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"restart-provider","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"done"}}}}`)
			_, _ = fmt.Fprintf(os.Stdout, `{"jsonrpc":"2.0","id":%s,"result":{"stopReason":"end_turn"}}`+"\n", request.ID)
		default:
			if len(request.ID) > 0 {
				_, _ = fmt.Fprintf(os.Stdout, `{"jsonrpc":"2.0","id":%s,"result":{}}`+"\n", request.ID)
			}
		}
	}
	os.Exit(0)
}

// Startup health reconnects each chat with a bounded context that it cancels
// as soon as the check returns. The reattached controller must outlive that
// context and keep the session live (#572).
func TestReconnectOnlyAfterDaemonRestartKeepsControllerLive(t *testing.T) {
	for _, tc := range []struct {
		name string
		// promptAtShutdown leaves a turn running when the first daemon detaches.
		promptAtShutdown bool
		// releaseWhileDetached lets that turn finish before the reconnect.
		releaseWhileDetached bool
	}{
		{name: "idle"},
		{name: "prompt running across restart", promptAtShutdown: true},
		{name: "prompt finished while detached", promptAtShutdown: true, releaseWhileDetached: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			testReconnectAfterDaemonRestart(t, tc.promptAtShutdown, tc.releaseWhileDetached)
		})
	}
}

func testReconnectAfterDaemonRestart(t *testing.T, promptAtShutdown, releaseWhileDetached bool) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	st := openStore(t)
	dataDir, workspace := t.TempDir(), t.TempDir()
	releasePath := filepath.Join(dataDir, "release")
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := acp.Config{
		Harness:      domain.HarnessClaudeCode,
		Capabilities: productionCaps(),
		Probe:        func(context.Context) error { return nil },
		Launch: func(context.Context, acp.LaunchConfig) (acp.Launch, error) {
			return acp.Launch{
				Command: os.Args[0], Args: []string{"-test.run=^TestRestartProviderHelper$"},
				Env: map[string]string{"AO_TEST_CHAT_RESTART_PROVIDER": "1", "AO_TEST_CHAT_RESTART_RELEASE": releasePath},
			}, nil
		},
	}
	var ids atomic.Uint64
	newService := func(activity *recordingActivity) *chatsvc.Service {
		svc := chatsvc.New(chatsvc.Options{
			Store: st, Reader: fullSnapshotReader(st), Sessions: st,
			Drivers: fakeRegistry{driver: acp.New(cfg, logger)}, Activity: activity, Log: logger,
			NewID: func() string { return fmt.Sprintf("restart-%d", ids.Add(1)) }, DataDir: dataDir,
		})
		t.Cleanup(func() {
			stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer stopCancel()
			svc.StopAll(stopCtx)
		})
		return svc
	}
	t.Cleanup(func() {
		_ = os.WriteFile(releasePath, nil, 0o600)
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 6*time.Second)
		defer stopCancel()
		if err := persistenthost.Shutdown(stopCtx, dataDir, string(testSession)); err != nil {
			t.Errorf("host cleanup: %v", err)
		}
	})

	firstService := newService(&recordingActivity{})
	start := chatsvc.StartConfig{
		SessionID: testSession, ProjectID: testProject, Kind: domain.KindOrchestrator,
		Harness: domain.HarnessClaudeCode, DataDir: dataDir, WorkspacePath: workspace,
	}
	first, err := firstService.Start(ctx, start)
	if err != nil {
		t.Fatal(err)
	}
	if promptAtShutdown {
		if _, err := first.Send(ctx, ports.ChatUserMessage{Text: "work through the restart"}); err != nil {
			t.Fatal(err)
		}
		awaitACPReplay(ctx, t, "turn running", func() bool { return first.State() == ports.ChatControllerBusy })
	}
	hostBefore := readACPReplayHost(t, dataDir)
	stopCtx, stopCancel := context.WithTimeout(ctx, 5*time.Second)
	firstService.StopAll(stopCtx)
	stopCancel()
	if releaseWhileDetached {
		if err := os.WriteFile(releasePath, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		awaitACPReplay(ctx, t, "detached turn finished", func() bool {
			_, statErr := os.Stat(releasePath)
			return os.IsNotExist(statErr)
		})
		time.Sleep(100 * time.Millisecond)
	}

	activity := &recordingActivity{}
	start.ProviderConversationID = first.ProviderConversationID()
	start.ReconnectOnly = true
	startupCtx, startupCancel := context.WithTimeout(ctx, 30*time.Second)
	second, err := newService(activity).Start(startupCtx, start)
	startupCancel()
	if err != nil {
		t.Fatalf("reconnect: %v", err)
	}
	if hostAfter := readACPReplayHost(t, dataDir); hostAfter.PID != hostBefore.PID {
		t.Fatal("reconnect replaced the persistent host")
	}
	if promptAtShutdown && !releaseWhileDetached {
		if err := os.WriteFile(releasePath, nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if promptAtShutdown {
		awaitACPReplay(ctx, t, "reattached controller settled the turn", func() bool {
			return second.State() != ports.ChatControllerBusy
		})
	}
	time.Sleep(300 * time.Millisecond)
	if state := second.State(); state == ports.ChatControllerStopped {
		t.Fatalf("reattached controller stopped; signals=%+v", activity.snapshot())
	}
	for _, signal := range activity.snapshot() {
		if signal.State == domain.ActivityExited {
			t.Fatalf("reattached controller reported %s; signals=%+v", signal.Event, activity.snapshot())
		}
	}
	if _, err := second.Send(ctx, ports.ChatUserMessage{Text: "after restart"}); err != nil {
		t.Fatalf("send after restart: %v", err)
	}
}
