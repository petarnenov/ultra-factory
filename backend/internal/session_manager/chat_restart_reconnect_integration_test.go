package sessionmanager

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	claudeagent "github.com/aoagents/agent-orchestrator/backend/internal/adapters/agent/claudecode"
	"github.com/aoagents/agent-orchestrator/backend/internal/adapters/chatdriver/acp"
	"github.com/aoagents/agent-orchestrator/backend/internal/adapters/chatdriver/persistenthost"
	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/lifecycle"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	browsersvc "github.com/aoagents/agent-orchestrator/backend/internal/service/browser"
	chatsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/chat"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/sqlitetest"
)

// The test binary doubles as the detached chat host, as AO's executable does.
func TestMain(m *testing.M) {
	if len(os.Args) >= 8 && os.Args[1] == "chat-host" && os.Args[7] == "--" {
		err := persistenthost.Run(context.Background(), persistenthost.Config{
			SessionID: os.Args[2], DataDir: os.Args[3], Workdir: os.Args[4],
			Env: os.Environ(), Argv: os.Args[8:], Protocol: persistenthost.Protocol(os.Args[5]),
			OwnershipFingerprint: os.Args[6],
		})
		if err != nil {
			_, _ = fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// TestRestartACPProviderHelper is a minimal ACP agent that answers each
// prompt once the release file exists.
func TestRestartACPProviderHelper(t *testing.T) {
	if os.Getenv("AO_TEST_RESTART_ACP_PROVIDER") != "1" {
		return
	}
	release := os.Getenv("AO_TEST_RESTART_ACP_RELEASE")
	started := os.Getenv("AO_TEST_RESTART_ACP_STARTED")
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
			_ = os.WriteFile(started, nil, 0o600)
			for {
				if _, err := os.Stat(release); err == nil {
					break
				}
				time.Sleep(5 * time.Millisecond)
			}
			_ = os.Remove(release)
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

type restartDaemon struct {
	chat    *chatsvc.Service
	manager *Manager
}

type transientReconnectFailure struct {
	ChatLauncher
	failed bool
}

func (l *transientReconnectFailure) StartChat(ctx context.Context, cfg ChatStart) (ChatStarted, error) {
	if cfg.ReconnectOnly && !l.failed {
		l.failed = true
		return ChatStarted{}, context.DeadlineExceeded
	}
	return l.ChatLauncher.StartChat(ctx, cfg)
}

func startRestartDaemon(t *testing.T, st *sqlite.Store, dataDir, workspace, release, started string, ids *atomic.Int64) restartDaemon {
	t.Helper()
	driver := acp.New(acp.Config{
		Harness: domain.HarnessClaudeCode,
		Capabilities: ports.ChatCapabilities{
			ports.ChatCapabilityStreaming: true, ports.ChatCapabilityApprovals: true,
			ports.ChatCapabilityInterrupt: true, ports.ChatCapabilityResume: true,
		},
		Probe: func(context.Context) error { return nil },
		Launch: func(context.Context, acp.LaunchConfig) (acp.Launch, error) {
			return acp.Launch{
				Command: os.Args[0], Args: []string{"-test.run=^TestRestartACPProviderHelper$"},
				Env: map[string]string{
					"AO_TEST_RESTART_ACP_PROVIDER": "1", "AO_TEST_RESTART_ACP_RELEASE": release,
					"AO_TEST_RESTART_ACP_STARTED": started,
				},
			}, nil
		},
	}, slog.New(slog.DiscardHandler))
	lcm := lifecycle.New(st, nil)
	chat := chatsvc.New(chatsvc.Options{
		Store: st, Sessions: st, Reader: chatsvc.SnapshotReaderFunc(func(ctx context.Context, id string) (chatsvc.ConversationRows, error) {
			rows, err := st.LoadConversationSnapshot(ctx, id)
			return chatsvc.ConversationRows{
				Conversation: rows.Conversation, ActiveBranch: rows.ActiveBranch,
				Turns: rows.Turns, Messages: rows.Messages, Activities: rows.Activities,
			}, err
		}),
		Drivers:  integrationChatRegistry{domain.HarnessClaudeCode: driver},
		Activity: lcm, Progress: lcm,
		Log:     slog.New(slog.DiscardHandler),
		NewID:   func() string { return fmt.Sprintf("restart-%d", ids.Add(1)) },
		DataDir: dataDir,
	})
	t.Cleanup(func() { chat.StopAll(context.Background()) })
	manager := New(Deps{
		Runtime: &fakeRuntime{}, Agents: singleAgent{agent: claudeagent.New()}, Workspace: &fakeWorkspace{path: workspace},
		Store: st, Messenger: &fakeMessenger{},
		Chat:      integrationChatLauncher{service: chat},
		Lifecycle: lcm, DataDir: dataDir,
		LookPath: func(string) (string, error) { return "/bin/true", nil },
		Logger:   slog.New(slog.DiscardHandler),
	})
	manager.browserCapabilities = browsersvc.NewAuthority()
	return restartDaemon{chat: chat, manager: manager}
}

// An orchestrator chat whose persistent host survives a daemon restart must be
// reattached by the new daemon, not left exited (#572).
func TestDaemonRestartReattachesSurvivingChatHost(t *testing.T) {
	for _, tc := range []struct {
		name                 string
		promptAtShutdown     bool
		releaseWhileDetached bool
		failFirstReconnect   bool
	}{
		{name: "idle"},
		{name: "prompt running across restart", promptAtShutdown: true},
		{name: "prompt finished while detached", promptAtShutdown: true, releaseWhileDetached: true},
		{name: "transient startup reconnect failure", failFirstReconnect: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			testDaemonRestartReattach(t, tc.promptAtShutdown, tc.releaseWhileDetached, tc.failFirstReconnect)
		})
	}
}

func testDaemonRestartReattach(t *testing.T, promptAtShutdown, releaseWhileDetached, failFirstReconnect bool) {
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	dataDir := t.TempDir()
	release := filepath.Join(dataDir, "release")
	startedPath := filepath.Join(dataDir, "started")
	st := sqlitetest.MustOpenAt(t, dataDir)
	if err := st.UpsertProject(ctx, domain.ProjectRecord{
		ID: string(chatTestProject), Path: dataDir, Config: testRoleAgents(), RegisteredAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	rec, err := st.CreateSession(ctx, domain.SessionRecord{
		ProjectID: chatTestProject, Kind: domain.KindOrchestrator, Harness: domain.HarnessClaudeCode,
		Mode:          domain.SessionModeChat,
		Metadata:      domain.SessionMetadata{WorkspacePath: t.TempDir(), Branch: "ao/orchestrator"},
		Activity:      domain.Activity{State: domain.ActivityIdle, LastActivityAt: now},
		FirstSignalAt: now, CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.WriteFile(release, nil, 0o600)
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 6*time.Second)
		defer stopCancel()
		if err := persistenthost.Shutdown(stopCtx, dataDir, string(rec.ID)); err != nil {
			t.Errorf("host cleanup: %v", err)
		}
	})
	var ids atomic.Int64

	first := startRestartDaemon(t, st, dataDir, rec.Metadata.WorkspacePath, release, startedPath, &ids)
	started, err := first.chat.StartChat(ctx, ChatStart{
		SessionID: rec.ID, ProjectID: rec.ProjectID, Kind: rec.Kind, Harness: rec.Harness,
		DataDir: dataDir, WorkspacePath: rec.Metadata.WorkspacePath,
		ControllerReady: func(started ChatStarted) (ChatControllerCommit, error) {
			metadata := rec.Metadata
			metadata.ProviderConversationID = started.ProviderConversationID
			metadata.ControllerGeneration = started.ControllerGeneration
			return ChatControllerCommit{Conversation: started.Conversation}, lifecycle.New(st, nil).MarkSpawned(ctx, rec.ID, metadata)
		},
	})
	if err != nil {
		t.Fatalf("first daemon start: %v", err)
	}
	if promptAtShutdown {
		if _, err := first.chat.StartChatTurn(ctx, rec.ID, "work through the restart"); err != nil {
			t.Fatal(err)
		}
		waitForRestartFile(ctx, t, startedPath, true)
	}
	first.chat.StopAll(ctx)
	if releaseWhileDetached {
		if err := os.WriteFile(release, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		waitForRestartFile(ctx, t, release, false)
	}

	second := startRestartDaemon(t, st, dataDir, rec.Metadata.WorkspacePath, release, startedPath, &ids)
	if failFirstReconnect {
		second.manager.chat = &transientReconnectFailure{ChatLauncher: second.manager.chat}
		second.manager.startupChatReconnectRetryDelays = []time.Duration{10 * time.Millisecond}
	}
	if err := second.manager.ReconcileBackground(ctx); err != nil {
		t.Fatalf("second daemon reconcile: %v", err)
	}
	if promptAtShutdown && !releaseWhileDetached {
		if err := os.WriteFile(release, nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if failFirstReconnect {
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) && !second.chat.HasLiveChatController(rec.ID) {
			time.Sleep(20 * time.Millisecond)
		}
	} else {
		time.Sleep(500 * time.Millisecond)
	}
	after, _, err := st.GetSession(ctx, rec.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !second.chat.HasLiveChatController(rec.ID) || after.Activity.State == domain.ActivityExited {
		t.Fatalf("startup did not keep the surviving host attached: provider=%s activity=%+v readiness=%s",
			started.ProviderConversationID, after.Activity, second.manager.SessionStatusReadiness(after))
	}
}

func waitForRestartFile(ctx context.Context, t *testing.T, path string, wantExists bool) {
	t.Helper()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		_, err := os.Stat(path)
		exists := err == nil
		if exists == wantExists {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatalf("timed out waiting for file %s exists=%t: %v", path, wantExists, ctx.Err())
		case <-ticker.C:
		}
	}
}
