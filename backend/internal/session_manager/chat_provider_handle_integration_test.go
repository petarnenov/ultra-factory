package sessionmanager

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/lifecycle"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	browsersvc "github.com/aoagents/agent-orchestrator/backend/internal/service/browser"
	chatsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/chat"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/sqlitetest"
)

// A replacement orchestrator's fresh provider handle must reach the session
// and the inherited project branch together (#572).
func TestReplacementOrchestratorPublishesFreshProviderHandleToActiveBranch(t *testing.T) {
	ctx := context.Background()
	st, dataDir := openHandleTestStore(t)
	previous, err := st.CreateSession(ctx, domain.SessionRecord{
		ProjectID: chatTestProject, Kind: domain.KindOrchestrator,
		Harness: domain.HarnessCodex, Mode: domain.SessionModeChat, IsTerminated: true,
		Metadata:  domain.SessionMetadata{ProviderConversationID: "thread-old"},
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("seed previous orchestrator: %v", err)
	}
	project, err := st.CreateConversation(ctx, "project-narrative",
		domain.ConversationScopeProject, chatTestProject, previous.ID, time.Now().UTC())
	if err != nil {
		t.Fatalf("seed project conversation: %v", err)
	}
	if err := st.UpsertActivity(ctx, project.ID, "", domain.ConversationActivity{
		ID: "earlier-work", Kind: domain.ActivityKindSystem,
		Status: domain.ActivityStatusCompleted, Summary: "earlier orchestrator work",
	}, time.Now().UTC()); err != nil {
		t.Fatalf("seed project history: %v", err)
	}

	// Input queued while the provider connects must stay on the new branch.
	provider := newIntegrationChatConversation("thread-new")
	var chat *chatsvc.Service
	driver := queueingStartDriver{
		integrationChatDriver: integrationDriverFor(provider),
		onStart: func(start ports.ChatStartConfig) {
			if _, err := chat.QueueUserMessage(ctx, start.SessionID, ports.ChatUserMessage{
				Text: "typed during startup", Origin: domain.MessageOriginHuman,
			}); err != nil {
				t.Errorf("queue during provider startup: %v", err)
			}
		},
	}
	m, chat := newHandleTestManager(t, st, dataDir, driver)

	cfg := asyncChatSpawnConfig("opening brief")
	cfg.Kind = domain.KindOrchestrator
	spawned, _, _, err := m.Spawn(ctx, cfg)
	if err != nil {
		t.Fatalf("spawn replacement orchestrator: %v", err)
	}

	session, found, err := st.GetSession(ctx, spawned.ID)
	if err != nil || !found {
		t.Fatalf("read replacement orchestrator: found=%v err=%v", found, err)
	}
	conversation, err := st.ConversationForSession(ctx, spawned.ID)
	if err != nil {
		t.Fatal(err)
	}
	if conversation.ID != project.ID {
		t.Fatalf("conversation = %s, want inherited %s", conversation.ID, project.ID)
	}
	active, err := st.ConversationBranch(ctx, conversation.ID, conversation.ActiveBranchID)
	if err != nil {
		t.Fatal(err)
	}
	if session.Metadata.ProviderConversationID != "thread-new" ||
		active.ProviderConversationID != session.Metadata.ProviderConversationID {
		t.Fatalf("session handle = %q, active branch %s handle = %q; want both thread-new",
			session.Metadata.ProviderConversationID, active.ID, active.ProviderConversationID)
	}
	snapshot, err := st.LoadConversationSnapshot(ctx, conversation.ID)
	if err != nil {
		t.Fatal(err)
	}
	var visible []string
	for _, message := range snapshot.Messages {
		visible = append(visible, message.Text)
	}
	if !slices.Contains(visible, "opening brief") || !slices.Contains(visible, "typed during startup") {
		t.Fatalf("visible messages on branch %s = %q, want opening brief and startup message", active.ID, visible)
	}
	provider.mu.Lock()
	sent := slices.Clone(provider.sent)
	provider.mu.Unlock()
	if len(sent) == 0 || sent[0].Text != "opening brief" ||
		slices.IndexFunc(sent[1:], func(m ports.ChatUserMessage) bool { return m.Text == "opening brief" }) >= 0 {
		t.Fatalf("provider prompts = %+v, want opening brief first and once", sent)
	}

	// The persisted pair must survive the resume check that #572 tripped on.
	if err := chat.Stop(ctx, spawned.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := chat.Start(ctx, chatsvc.StartConfig{
		SessionID: spawned.ID, ProjectID: chatTestProject, Kind: domain.KindOrchestrator,
		Harness: domain.HarnessCodex, WorkspacePath: t.TempDir(),
		ProviderConversationID: session.Metadata.ProviderConversationID,
		HistoryMode:            ports.ChatHistoryDeferred,
	}); err != nil {
		t.Fatalf("resume replacement orchestrator: %v", err)
	}
}

// A persisted handle mismatch fails Resume with the typed error that the
// API maps to 409 instead of an unclassified 500 (#572).
func TestResumeAgentReportsPersistedProviderHandleMismatch(t *testing.T) {
	ctx := context.Background()
	st, dataDir := openHandleTestStore(t)
	session, err := st.CreateSession(ctx, domain.SessionRecord{
		ProjectID: chatTestProject, Kind: domain.KindOrchestrator,
		Harness: domain.HarnessCodex, Mode: domain.SessionModeChat,
		Activity: domain.Activity{State: domain.ActivityExited, LastActivityAt: time.Now().UTC()},
		Metadata: domain.SessionMetadata{
			ProviderConversationID: "thread-old", WorkspacePath: t.TempDir(), Branch: "ao/orchestrator",
		},
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("seed orchestrator: %v", err)
	}
	if _, err := st.CreateConversation(ctx, "project-narrative",
		domain.ConversationScopeProject, chatTestProject, session.ID, time.Now().UTC()); err != nil {
		t.Fatalf("seed project conversation: %v", err)
	}
	session.Metadata.ProviderConversationID = "thread-new"
	if err := st.UpdateSession(ctx, session); err != nil {
		t.Fatalf("diverge session handle: %v", err)
	}

	m, _ := newHandleTestManager(t, st, dataDir, integrationDriverFor(newIntegrationChatConversation("thread-new")))
	if _, err := m.ResumeAgentWithMode(ctx, session.ID); !errors.Is(err, ports.ErrChatProviderHandleMismatch) {
		t.Fatalf("resume error = %v, want provider handle mismatch", err)
	}
}

func openHandleTestStore(t *testing.T) (*sqlite.Store, string) {
	t.Helper()
	dataDir := t.TempDir()
	st := sqlitetest.MustOpenAt(t, dataDir)
	if err := st.UpsertProject(context.Background(), domain.ProjectRecord{
		ID: string(chatTestProject), Path: dataDir, Config: testRoleAgents(), RegisteredAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	return st, dataDir
}

// queueingStartDriver runs onStart after Chat reserved the boundary and
// before ControllerReady publishes it.
type queueingStartDriver struct {
	integrationChatDriver
	onStart func(ports.ChatStartConfig)
}

func (d queueingStartDriver) Start(ctx context.Context, cfg ports.ChatStartConfig) (ports.ChatConversation, error) {
	d.onStart(cfg)
	return d.integrationChatDriver.Start(ctx, cfg)
}

func integrationDriverFor(provider *integrationChatConversation) integrationChatDriver {
	return integrationChatDriver{
		harness: domain.HarnessCodex,
		start:   func() ports.ChatConversation { return provider },
		resume:  func() ports.ChatConversation { return provider },
	}
}

func newHandleTestManager(
	t *testing.T,
	st *sqlite.Store,
	dataDir string,
	driver ports.ChatDriver,
) (*Manager, *chatsvc.Service) {
	t.Helper()
	var nextID atomic.Int64
	chat := chatsvc.New(chatsvc.Options{
		Store: st, Sessions: st,
		Drivers: integrationChatRegistry{domain.HarnessCodex: driver},
		Log:     slog.New(slog.DiscardHandler),
		NewID:   func() string { return fmt.Sprintf("handle-%d", nextID.Add(1)) },
	})
	t.Cleanup(func() { chat.StopAll(context.Background()) })
	m := New(Deps{
		Runtime: &fakeRuntime{}, Agents: fakeAgents{}, Workspace: &fakeWorkspace{path: t.TempDir()},
		Store: st, Messenger: &fakeMessenger{},
		Chat:      realQueueDrainLauncher{integrationChatLauncher{service: chat}},
		Lifecycle: lifecycle.New(st, nil), DataDir: dataDir,
		LookPath: func(string) (string, error) { return "/bin/true", nil },
		Logger:   slog.New(slog.DiscardHandler),
	})
	m.browserCapabilities = browsersvc.NewAuthority()
	m.runBackground = func(work func()) { work() }
	return m, chat
}
