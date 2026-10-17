package sessionmanager

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

func TestObserveQuotaAndSnapshotIsFreshThresholdedAndIdempotent(t *testing.T) {
	const now = int64(1_792_147_200)
	clock := time.Unix(now, 0).UTC()
	store := newFakeStore()
	store.sessions["session-1"] = domain.SessionRecord{
		ID: "session-1", Harness: domain.HarnessCodex,
		Metadata: domain.SessionMetadata{
			Prompt: "finish task", AgentSessionID: "native-1", RuntimeLaunchID: "run-1", NativeTranscriptPath: "/transcripts/native-1.jsonl",
		},
	}
	dataDir := t.TempDir()
	m := New(Deps{Store: store, DataDir: dataDir, Clock: func() time.Time { return clock }})
	source := QuotaSnapshotSource{Harness: domain.HarnessCodex, Generation: "run-1", NativeRootID: "native-1", NativeTranscriptPath: "/transcripts/native-1.jsonl"}

	if err := m.ObserveQuotaAndSnapshot(context.Background(), "session-1", source, 10.01, clock); err != nil {
		t.Fatal(err)
	}
	if entries, _ := filepath.Glob(filepath.Join(dataDir, "quota-snapshots", "session-1", "*.json")); len(entries) != 0 {
		t.Fatalf("snapshot created above threshold: %v", entries)
	}
	stale := clock.Add(-quotaObservationMaxAge - time.Second)
	if err := m.ObserveQuotaAndSnapshot(context.Background(), "session-1", source, 10, stale); err != nil {
		t.Fatal(err)
	}
	if err := m.ObserveQuotaAndSnapshot(context.Background(), "session-1", source, 10, clock); err != nil {
		t.Fatal(err)
	}
	entries, err := filepath.Glob(filepath.Join(dataDir, "quota-snapshots", "session-1", "*.json"))
	if err != nil || len(entries) != 1 {
		t.Fatalf("snapshot files=%v err=%v", entries, err)
	}
	before, err := os.ReadFile(entries[0])
	if err != nil {
		t.Fatal(err)
	}
	var snapshot quotaSnapshot
	if err := json.Unmarshal(before, &snapshot); err != nil {
		t.Fatal(err)
	}
	if snapshot.RemainingPercent != 10 || snapshot.OriginalTask != "finish task" || snapshot.Generation != "run-1" {
		t.Fatalf("snapshot = %+v", snapshot)
	}
	if len(before) > quotaSnapshotMaxBytes {
		t.Fatalf("snapshot size = %d", len(before))
	}
	if err := m.ObserveQuotaAndSnapshot(context.Background(), "session-1", source, 1, clock); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(entries[0])
	if err != nil || string(after) != string(before) {
		t.Fatalf("snapshot changed across duplicate trigger: err=%v", err)
	}
	if err := m.removeQuotaSnapshot(context.Background(), "session-1", "run-1"); err != nil {
		t.Fatal(err)
	}
	if entries, _ := filepath.Glob(filepath.Join(dataDir, "quota-snapshots", "session-1", "*.json")); len(entries) != 0 {
		t.Fatalf("matching snapshot retained after completed handoff cleanup: %v", entries)
	}
}

func TestObserveQuotaAndSnapshotRejectsSupersededGeneration(t *testing.T) {
	clock := time.Now().UTC()
	store := newFakeStore()
	store.sessions["session-1"] = domain.SessionRecord{
		ID: "session-1", Harness: domain.HarnessCodex,
		Metadata: domain.SessionMetadata{AgentSessionID: "native-new", RuntimeLaunchID: "run-new", NativeTranscriptPath: "/transcripts/native-new.jsonl"},
	}
	m := New(Deps{Store: store, DataDir: t.TempDir(), Clock: func() time.Time { return clock }})
	err := m.ObserveQuotaAndSnapshot(context.Background(), "session-1", QuotaSnapshotSource{
		Harness: domain.HarnessCodex, Generation: "run-old", NativeRootID: "native-old", NativeTranscriptPath: "/transcripts/native-old.jsonl",
	}, 2, clock)
	if err != nil {
		t.Fatal(err)
	}
	entries, _ := filepath.Glob(filepath.Join(m.dataDir, "quota-snapshots", "session-1", "*.json"))
	if len(entries) != 0 {
		t.Fatalf("stale generation created snapshot: %v", entries)
	}
}
