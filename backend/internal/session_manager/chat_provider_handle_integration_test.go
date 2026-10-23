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
	turnForText := func(text string) string {
		for _, message := range snapshot.Messages {
			if message.Text == text {
				return message.TurnID
			}
		}
		return ""
	}
	briefTurnID := turnForText("opening brief")
	startupTurnID := turnForText("typed during startup")
	if briefTurnID == "" || startupTurnID == "" {
		t.Fatalf("opening brief turn = %q, startup turn = %q", briefTurnID, startupTurnID)
	}
	briefTurn, err := st.TurnByID(ctx, briefTurnID)
	if err != nil || briefTurn.ProviderTurnID == "" {
		t.Fatalf("opening brief provider turn = %q, err = %v", briefTurn.ProviderTurnID, err)
	}
	for _, turnID := range []string{briefTurnID, startupTurnID} {
		if _, err := st.ConversationEditAnchor(ctx, conversation.ID, turnID); err != nil {
			t.Errorf("edit anchor for queued turn %s: %v", turnID, err)
		}
	}
	later, err := chat.Send(ctx, spawned.ID, ports.ChatUserMessage{
		Text: "later prompt", Origin: domain.MessageOriginHuman,
	})
	if err != nil {
		t.Fatalf("send later prompt: %v", err)
	}
	laterAnchor, err := st.ConversationEditAnchor(ctx, conversation.ID, later.ID)
	if err != nil {
		t.Fatalf("edit anchor for later prompt: %v", err)
	}
	if laterAnchor.PreviousProviderTurnID != briefTurn.ProviderTurnID {
		t.Fatalf("later prompt previous provider turn = %q, want opening brief provider turn %q",
			laterAnchor.PreviousProviderTurnID, briefTurn.ProviderTurnID)
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

func TestWakeWithFreshProviderMovesUndispatchedQueueIntoProviderScope(t *testing.T) {
	ctx := context.Background()
	st, _ := openHandleTestStore(t)
	now := time.Now().UTC()
	session, err := st.CreateSession(ctx, domain.SessionRecord{
		ProjectID: chatTestProject, Kind: domain.KindOrchestrator,
		Harness: domain.HarnessCodex, Mode: domain.SessionModeChat,
		Metadata:  domain.SessionMetadata{ProviderConversationID: "thread-retired"},
		Activity:  domain.Activity{State: domain.ActivityIdle, LastActivityAt: now},
		CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatalf("seed hibernated orchestrator: %v", err)
	}
	conversation, err := st.CreateConversation(ctx, "wake-project-narrative",
		domain.ConversationScopeProject, chatTestProject, session.ID, now)
	if err != nil {
		t.Fatalf("seed project conversation: %v", err)
	}
	hibernatedAt := now.Add(-time.Minute)
	session.UpdatedAt = now
	if err := st.UpdateSession(ctx, session); err != nil {
		t.Fatalf("update hibernated orchestrator: %v", err)
	}
	current, found, err := st.GetSession(ctx, session.ID)
	if err != nil || !found {
		t.Fatalf("read orchestrator before hibernating: found=%v err=%v", found, err)
	}
	if applied, err := st.SetSessionHibernated(ctx, session.ID, current.Revision, &hibernatedAt); err != nil || !applied {
		t.Fatalf("mark orchestrator hibernated: applied=%v err=%v", applied, err)
	}

	provider := newIntegrationChatConversation("thread-wake-fresh")
	var chat *chatsvc.Service
	driver := queueingResumeDriver{
		integrationChatDriver: integrationDriverFor(provider),
		onResume: func(start ports.ChatResumeConfig) {
			if _, err := chat.QueueUserMessage(ctx, start.SessionID, ports.ChatUserMessage{
				Text: "typed during startup", Origin: domain.MessageOriginHuman,
			}); err != nil {
				t.Errorf("queue during provider startup: %v", err)
			}
		},
	}
	var nextID atomic.Int64
	chat = chatsvc.New(chatsvc.Options{
		Store: st, Sessions: st,
		Drivers: integrationChatRegistry{domain.HarnessCodex: driver},
		Log:     slog.New(slog.DiscardHandler),
		NewID:   func() string { return fmt.Sprintf("wake-queue-%d", nextID.Add(1)) },
	})
	t.Cleanup(func() { chat.StopAll(context.Background()) })
	chat.SetWakeCallback(func(wakeCtx context.Context, id domain.SessionID) error {
		current, found, err := st.GetSession(wakeCtx, id)
		if err != nil || !found {
			return fmt.Errorf("read hibernated session: found=%v err=%w", found, err)
		}
		if cleared, err := st.SetSessionHibernated(wakeCtx, id, current.Revision, nil); err != nil || !cleared {
			return fmt.Errorf("clear hibernation: applied=%v err=%w", cleared, err)
		}
		_, err = chat.Start(wakeCtx, chatsvc.StartConfig{
			SessionID: id, ProjectID: chatTestProject, Kind: domain.KindOrchestrator,
			Harness: domain.HarnessCodex, WorkspacePath: t.TempDir(),
			ProviderConversationID: "thread-retired", ProviderScopeID: "wake-provider-scope",
			FreshIfProviderConversationMissing: true, HistoryMode: ports.ChatHistoryDeferred,
			ControllerReady: func(started ports.ChatControllerStarted) (ports.ChatControllerCommit, error) {
				current, found, readErr := st.GetSession(wakeCtx, id)
				if readErr != nil || !found {
					return ports.ChatControllerCommit{}, fmt.Errorf("read waking session: found=%v err=%w", found, readErr)
				}
				current.Metadata.ProviderConversationID = started.ProviderConversationID
				current.Metadata.ControllerGeneration = started.ControllerGeneration
				current.Activity = domain.Activity{State: domain.ActivityIdle, LastActivityAt: now}
				current.UpdatedAt = now
				if started.ProviderBoundary == nil {
					return ports.ChatControllerCommit{}, errors.New("wake did not reserve a fresh provider boundary")
				}
				if err := st.CommitChatSpawn(wakeCtx, current, *started.ProviderBoundary); err != nil {
					return ports.ChatControllerCommit{}, err
				}
				committed := started.Conversation
				committed.ActiveBranchID = started.ProviderBoundary.ID
				return ports.ChatControllerCommit{
					Conversation: committed, ControllerOwner: current.ControllerOwner(),
				}, nil
			},
		})
		if err != nil {
			t.Errorf("start fresh provider during wake: %v", err)
		}
		return err
	})

	opening, err := chat.QueueUserMessage(ctx, session.ID, ports.ChatUserMessage{
		Text: "opening brief", Origin: domain.MessageOriginHuman,
	})
	if err != nil || opening.State != domain.TurnStateQueued {
		t.Fatalf("queue opening brief before wake: turn=%+v err=%v", opening, err)
	}
	if err := chat.SetChatView(ctx, session.ID, "wake-view", true); err != nil {
		t.Fatalf("wake hibernated chat: %v", err)
	}
	if err := chat.DrainQueued(ctx, session.ID); err != nil {
		t.Fatalf("drain queue after wake: %v", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		provider.mu.Lock()
		sentCount := len(provider.sent)
		provider.mu.Unlock()
		if sentCount >= 1 && chat.HasLiveChatController(session.ID) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	provider.mu.Lock()
	sent := slices.Clone(provider.sent)
	provider.mu.Unlock()
	if len(sent) != 1 || sent[0].Text != "opening brief" {
		t.Fatalf("fresh provider prompts = %+v, want opening brief dispatched first", sent)
	}

	snapshot, err := st.LoadConversationSnapshot(ctx, conversation.ID)
	if err != nil {
		t.Fatal(err)
	}
	turnForText := func(text string) string {
		for _, message := range snapshot.Messages {
			if message.Text == text {
				return message.TurnID
			}
		}
		return ""
	}
	briefTurnID := turnForText("opening brief")
	startupTurnID := turnForText("typed during startup")
	briefTurn, err := st.TurnByID(ctx, briefTurnID)
	if err != nil || briefTurn.ProviderTurnID == "" {
		t.Fatalf("opening brief provider turn = %q, err = %v", briefTurn.ProviderTurnID, err)
	}
	for _, turnID := range []string{briefTurnID, startupTurnID} {
		if turnID == "" {
			t.Fatal("queued prompt is missing its conversation turn")
		}
		if _, err := st.ConversationEditAnchor(ctx, conversation.ID, turnID); err != nil {
			t.Errorf("edit anchor for queued turn %s: %v", turnID, err)
		}
	}
	later, err := chat.Send(ctx, session.ID, ports.ChatUserMessage{
		Text: "later prompt", Origin: domain.MessageOriginHuman,
	})
	if err != nil {
		t.Fatalf("send later prompt: %v", err)
	}
	laterAnchor, err := st.ConversationEditAnchor(ctx, conversation.ID, later.ID)
	if err != nil {
		t.Fatalf("edit anchor for later prompt: %v", err)
	}
	if laterAnchor.PreviousProviderTurnID != briefTurn.ProviderTurnID {
		t.Fatalf("later prompt previous provider turn = %q, want opening brief provider turn %q",
			laterAnchor.PreviousProviderTurnID, briefTurn.ProviderTurnID)
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

type queueingResumeDriver struct {
	integrationChatDriver
	onResume func(ports.ChatResumeConfig)
}

func (d queueingResumeDriver) Resume(ctx context.Context, cfg ports.ChatResumeConfig) (ports.ChatConversation, error) {
	d.onResume(cfg)
	return d.integrationChatDriver.Resume(ctx, cfg)
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
