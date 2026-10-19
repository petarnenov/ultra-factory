package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

func TestSessionProgressMergesSignalsAndKeepsHistory(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	seedProject(t, s, "progress")
	session, err := s.CreateSession(ctx, sampleRecord("progress"))
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	tool := "Bash"

	if err := s.RecordSessionProgress(ctx, session.ID, domain.SessionProgressUpdate{
		ProviderAt: now, InFlightTool: &tool, InFlightToolAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordSessionProgress(ctx, session.ID, domain.SessionProgressUpdate{
		ProviderAt: now.Add(-time.Minute), TerminalAt: now.Add(time.Minute),
	}); err != nil {
		t.Fatal(err)
	}
	progress, ok, err := s.GetSessionProgress(ctx, session.ID)
	if err != nil || !ok {
		t.Fatalf("get progress: ok=%v err=%v", ok, err)
	}
	if !progress.ProviderAt.Equal(now) {
		t.Fatalf("providerAt = %v, want the newest %v", progress.ProviderAt, now)
	}
	if !progress.TerminalAt.Equal(now.Add(time.Minute)) {
		t.Fatalf("terminalAt = %v", progress.TerminalAt)
	}
	if progress.InFlightTool != "Bash" {
		t.Fatalf("in-flight tool = %q", progress.InFlightTool)
	}

	if err := s.RecordSessionProgress(ctx, session.ID, domain.SessionProgressUpdate{ClearInFlight: true}); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordSessionProgress(ctx, session.ID, domain.SessionProgressUpdate{
		BlockedAt: now.Add(2 * time.Minute), BlockedReason: stringPointer("git_credential_missing"),
	}); err != nil {
		t.Fatal(err)
	}
	baseSeq, _ := s.LatestSeq(ctx)
	if err := s.RecordSessionProgress(ctx, session.ID, domain.SessionProgressUpdate{
		LastNotifiedFor: now, StallCountDelta: 1, LastStallAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	events, err := s.EventsAfter(ctx, baseSeq, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || string(events[0].Type) != "session_updated" {
		t.Fatalf("stall crossing events = %+v, want one session_updated", events)
	}
	progress, _, _ = s.GetSessionProgress(ctx, session.ID)
	if progress.InFlightTool != "" || progress.BlockedReason != "git_credential_missing" {
		t.Fatalf("progress after clear/block = %+v", progress)
	}
	if progress.StallCount != 1 || progress.LastNotifiedProgressAt.IsZero() {
		t.Fatalf("history = %+v", progress)
	}

	// Real progress after the blocked fact clears it.
	if err := s.RecordSessionProgress(ctx, session.ID, domain.SessionProgressUpdate{GitAt: now.Add(3 * time.Minute)}); err != nil {
		t.Fatal(err)
	}
	progress, _, _ = s.GetSessionProgress(ctx, session.ID)
	if progress.BlockedReason != "" || !progress.BlockedAt.IsZero() {
		t.Fatalf("blocked fact survived real progress: %+v", progress)
	}

	all, err := s.ListSessionProgress(ctx)
	if err != nil || all[session.ID].StallCount != 1 {
		t.Fatalf("list progress = %+v err=%v", all, err)
	}
}

func TestSessionProgressCDCEmitsOnInsertStallAndRecovery(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	seedProject(t, s, "progress-cdc")
	session, err := s.CreateSession(ctx, sampleRecord("progress-cdc"))
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	baseSeq, _ := s.LatestSeq(ctx)
	if err := s.RecordSessionProgress(ctx, session.ID, domain.SessionProgressUpdate{
		LastNotifiedFor: now, StallCountDelta: 1, LastStallAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	events, err := s.EventsAfter(ctx, baseSeq, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || string(events[0].Type) != "session_updated" {
		t.Fatalf("insert stall events = %+v, want one session_updated", events)
	}

	baseSeq, _ = s.LatestSeq(ctx)
	if err := s.RecordSessionProgress(ctx, session.ID, domain.SessionProgressUpdate{
		ClearLastNotified: true, LastStallFor: 5 * time.Minute,
	}); err != nil {
		t.Fatal(err)
	}
	events, err = s.EventsAfter(ctx, baseSeq, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || string(events[0].Type) != "session_updated" {
		t.Fatalf("recovery events = %+v, want one session_updated", events)
	}
}

func stringPointer(value string) *string { return &value }
