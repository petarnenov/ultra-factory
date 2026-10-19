package usage

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/adapters/agent/opencode"
	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/lifecycle"
)

// TestCollectorRegistersOpencodeV2StoreAsSnapshotSource proves the harness
// binds to its SQLite store rather than to a transcript.
func TestCollectorRegistersOpencodeV2StoreAsSnapshotSource(t *testing.T) {
	store := collectorTestStore(t)
	session := collectorTestSession(t, store, domain.HarnessOpenCodeV2, "ses_opencode", false)
	home := t.TempDir()
	collector := NewCollector(store, SourceRoots{OpencodeV2Home: home}, nil)
	collector.now = func() time.Time { return time.Unix(1700000000, 0).UTC() }
	signal := HookSignal{
		Harness:         domain.HarnessOpenCodeV2,
		Event:           "session-start",
		NativeSessionID: "ses_opencode",
		ModelID:         "deepseek-v4.1-flash",
	}

	if err := collector.RecordHook(context.Background(), session.ID, signal); err != nil {
		t.Fatalf("record start before the store exists: %v", err)
	}
	bindings, err := store.ListUsageBindingsForSession(context.Background(), session.ID)
	if err != nil || len(bindings) != 1 {
		t.Fatalf("bindings = %+v err = %v", bindings, err)
	}
	sources, err := store.ListUsageSourcesForBinding(context.Background(), bindings[0].ID)
	if err != nil || len(sources) != 0 {
		t.Fatalf("sources = %+v err = %v, want none without a store", sources, err)
	}

	storePath := filepath.Join(home, "opencode.db")
	mustNoError(t, os.WriteFile(storePath, []byte("store"), 0o600))
	if err := collector.RecordHook(context.Background(), session.ID, signal); err != nil {
		t.Fatalf("record start with the store present: %v", err)
	}
	sources, err = store.ListUsageSourcesForBinding(context.Background(), bindings[0].ID)
	if err != nil || len(sources) != 1 {
		t.Fatalf("sources = %+v err = %v", sources, err)
	}
	resolved, err := filepath.EvalSymlinks(storePath)
	mustNoError(t, err)
	if sources[0].Kind != domain.UsageSourceOpencodeV2 || sources[0].ArtifactPath != resolved ||
		sources[0].NativeSessionID != "ses_opencode" {
		t.Fatalf("source = %+v, want the resolved store path %q", sources[0], resolved)
	}
}

// TestCollectorDiscoversOpencodeV2StoreAfterSessionTerminated covers a hook
// arrived before the store, with no second hook before termination.
func TestCollectorDiscoversOpencodeV2StoreAfterSessionTerminated(t *testing.T) {
	store := collectorTestStore(t)
	session := collectorTestSession(t, store, domain.HarnessOpenCodeV2, "ses_opencode", false)
	home := t.TempDir()
	collector := NewCollector(store, SourceRoots{OpencodeV2Home: home}, nil)
	manager := lifecycle.New(store, nil)
	manager.SetUsageFinalizer(collector)

	if err := collector.RecordHook(context.Background(), session.ID, HookSignal{
		Harness:         domain.HarnessOpenCodeV2,
		Event:           "session-start",
		NativeSessionID: "ses_opencode",
	}); err != nil {
		t.Fatalf("record start before the store exists: %v", err)
	}
	bindings, err := store.ListUsageBindingsForSession(context.Background(), session.ID)
	if err != nil || len(bindings) != 1 || bindings[0].State != domain.UsageBindingActive {
		t.Fatalf("bindings = %+v err = %v, want one active binding", bindings, err)
	}
	assertNoUsageSourcesForSession(t, store, session.ID)

	// No second hook: termination is the only event left, and the lifecycle
	// finalizer is all it commits before the session is marked dead.
	mustNoError(t, manager.MarkTerminated(context.Background(), session.ID), "terminate session")
	bindings, err = store.ListUsageBindingsForSession(context.Background(), session.ID)
	if err != nil || len(bindings) != 1 || bindings[0].State != domain.UsageBindingFinalizing {
		t.Fatalf("bindings after termination = %+v err = %v, want finalizing", bindings, err)
	}

	storePath := filepath.Join(home, "opencode.db")
	mustNoError(t, os.WriteFile(storePath, []byte("store"), 0o600))
	mustNoError(t, collector.ReconcileSources(context.Background(), 8), "reconcile")

	sources, err := store.ListUsageSourcesForBinding(context.Background(), bindings[0].ID)
	if err != nil || len(sources) != 1 {
		t.Fatalf("sources = %+v err = %v, want the store discovered", sources, err)
	}
	resolved, err := filepath.EvalSymlinks(storePath)
	mustNoError(t, err)
	if sources[0].Kind != domain.UsageSourceOpencodeV2 || sources[0].ArtifactPath != resolved ||
		sources[0].NativeSessionID != "ses_opencode" {
		t.Fatalf("source = %+v, want the resolved store path %q", sources[0], resolved)
	}
}

// TestCollectorRejectsOpencodeV2StoreOutsideDataHome keeps the new artifact
// extension inside the same root check the JSONL harnesses use.
func TestCollectorRejectsOpencodeV2StoreOutsideDataHome(t *testing.T) {
	store := collectorTestStore(t)
	session := collectorTestSession(t, store, domain.HarnessOpenCodeV2, "ses_opencode", false)
	base := t.TempDir()
	home := filepath.Join(base, "opencode-v2-home")
	mustNoError(t, os.MkdirAll(home, 0o700))
	outside := filepath.Join(base, "elsewhere.db")
	mustNoError(t, os.WriteFile(outside, []byte("store"), 0o600))
	collector := NewCollector(store, SourceRoots{OpencodeV2Home: home}, nil)

	err := collector.RecordHook(context.Background(), session.ID, HookSignal{
		Harness:         domain.HarnessOpenCodeV2,
		Event:           "session-start",
		NativeSessionID: "ses_opencode",
		TranscriptPath:  outside,
	})
	if err == nil {
		t.Fatal("a store outside the OpenCode data home must be rejected")
	}
	if strings.Contains(err.Error(), outside) {
		t.Fatalf("validation error exposed the artifact path: %v", err)
	}
}

// TestDefaultSourceRootsMatchOpencodeAdapterDataHome keeps the usage service's
// copy of the OpenCode 2 data-home rule in step with the adapter that owns it.
func TestDefaultSourceRootsMatchOpencodeAdapterDataHome(t *testing.T) {
	want, err := opencode.V2DataHome()
	mustNoError(t, err)
	roots, err := DefaultSourceRoots(context.Background(), t.TempDir())
	mustNoError(t, err)
	if filepath.Dir(roots.OpencodeV2Home) != want {
		t.Fatalf("OpencodeV2Home = %q, want a child of %q", roots.OpencodeV2Home, want)
	}
}
