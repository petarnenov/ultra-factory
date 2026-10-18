package settings

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

type coverageSettingsStore struct {
	hibernationSettingsStore
	mode     domain.SessionMode
	cloud    bool
	modeErr  error
	cloudErr error
}

func (s *coverageSettingsStore) GetAppSettings(context.Context) (Snapshot, error) {
	if s.err != nil {
		return Snapshot{}, s.err
	}
	return Snapshot{
		DefaultSessionMode: s.mode,
		CloudOffering:      s.cloud,
	}, nil
}

func (s *coverageSettingsStore) SetDefaultSessionMode(_ context.Context, mode domain.SessionMode, _ time.Time) error {
	if s.modeErr != nil {
		return s.modeErr
	}
	s.mode = mode
	return nil
}

func (s *coverageSettingsStore) SetCloudOffering(_ context.Context, enabled bool, _ time.Time) error {
	if s.cloudErr != nil {
		return s.cloudErr
	}
	s.cloud = enabled
	return nil
}

type fakeChatCapability struct {
	supported map[domain.AgentHarness]bool
}

func (f fakeChatCapability) SupportsChat(harness domain.AgentHarness) bool {
	return f.supported[harness]
}

func TestServiceOfferingReturnsBootResolvedSettings(t *testing.T) {
	ctx := context.Background()
	offering := Offering{Client: "desktop", LocalEnabled: true, TrackerIntakeEnabled: true}
	svc := New(&coverageSettingsStore{}, nil, offering, nil)

	if got := svc.Offering(); got != offering {
		t.Fatalf("Offering() = %+v, want %+v", got, offering)
	}
	_ = ctx
}

func TestServiceDefaultSessionModeResolvesAndFallsBack(t *testing.T) {
	ctx := context.Background()

	store := &coverageSettingsStore{mode: domain.SessionModeChat}
	svc := New(store, nil, Offering{}, nil)
	if got := svc.DefaultSessionMode(ctx); got != domain.SessionModeChat {
		t.Fatalf("DefaultSessionMode = %q, want %q", got, domain.SessionModeChat)
	}

	store = &coverageSettingsStore{mode: ""}
	svc = New(store, nil, Offering{}, nil)
	if got := svc.DefaultSessionMode(ctx); got != domain.DefaultSessionMode {
		t.Fatalf("DefaultSessionMode(empty) = %q, want %q", got, domain.DefaultSessionMode)
	}

	store = &coverageSettingsStore{}
	store.err = errors.New("database unavailable")
	svc = New(store, nil, Offering{}, nil)
	if got := svc.DefaultSessionMode(ctx); got != domain.DefaultSessionMode {
		t.Fatalf("DefaultSessionMode(error) = %q, want %q", got, domain.DefaultSessionMode)
	}
}

func TestServiceSetDefaultSessionModeValidatesPersistsAndReportsErrors(t *testing.T) {
	ctx := context.Background()
	store := &coverageSettingsStore{}
	svc := New(store, nil, Offering{}, nil)

	if _, err := svc.SetDefaultSessionMode(ctx, domain.SessionMode("not-a-mode")); !errors.Is(err, ports.ErrChatUnsupported) {
		t.Fatalf("invalid mode err = %v, want ErrChatUnsupported", err)
	}

	snapshot, err := svc.SetDefaultSessionMode(ctx, domain.SessionModeChat)
	if err != nil {
		t.Fatal(err)
	}
	if store.mode != domain.SessionModeChat {
		t.Fatalf("stored mode = %q, want %q", store.mode, domain.SessionModeChat)
	}
	if snapshot.DefaultSessionMode != domain.SessionModeChat {
		t.Fatalf("returned mode = %q, want %q", snapshot.DefaultSessionMode, domain.SessionModeChat)
	}

	store.modeErr = errors.New("write failed")
	if _, err := svc.SetDefaultSessionMode(ctx, domain.SessionModeTUI); err == nil {
		t.Fatal("store write failure ignored")
	}
}

func TestServiceSetCloudOfferingFlipsToggleAndReportsErrors(t *testing.T) {
	ctx := context.Background()
	store := &coverageSettingsStore{}
	svc := New(store, nil, Offering{}, nil)

	snapshot, err := svc.SetCloudOffering(ctx, true)
	if err != nil {
		t.Fatal(err)
	}
	if !store.cloud || !snapshot.CloudOffering {
		t.Fatalf("cloud not enabled: store=%v snapshot=%v", store.cloud, snapshot.CloudOffering)
	}

	snapshot, err = svc.SetCloudOffering(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	if store.cloud || snapshot.CloudOffering {
		t.Fatalf("cloud not disabled: store=%v snapshot=%v", store.cloud, snapshot.CloudOffering)
	}

	store.cloudErr = errors.New("write failed")
	if _, err := svc.SetCloudOffering(ctx, true); err == nil {
		t.Fatal("store write failure ignored")
	}
}

func TestServiceChatHarnessesFiltersCandidates(t *testing.T) {
	ctx := context.Background()
	store := &coverageSettingsStore{}
	candidates := []domain.AgentHarness{domain.HarnessCodex, domain.HarnessAider}

	nilChatSvc := New(store, nil, Offering{}, nil)
	if got := nilChatSvc.ChatHarnesses(candidates); got != nil {
		t.Fatalf("nil chat capability = %+v, want nil", got)
	}

	chat := fakeChatCapability{supported: map[domain.AgentHarness]bool{domain.HarnessCodex: true}}
	svc := New(store, chat, Offering{}, nil)
	got := svc.ChatHarnesses(candidates)
	if !reflect.DeepEqual(got, []domain.AgentHarness{domain.HarnessCodex}) {
		t.Fatalf("ChatHarnesses = %+v, want [%s]", got, domain.HarnessCodex)
	}
	if got := svc.ChatHarnesses(nil); len(got) != 0 {
		t.Fatalf("ChatHarnesses(nil) = %+v, want empty", got)
	}
	_ = ctx
}
