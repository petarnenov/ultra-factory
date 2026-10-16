package store_test

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

func TestActiveRemoteSpawnedSessionsSkipTerminatedSessions(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	seedProject(t, s, "ao")
	live, err := s.CreateSession(ctx, sampleRecord("ao"))
	if err != nil {
		t.Fatal(err)
	}
	local, err := s.CreateSession(ctx, sampleRecord("ao"))
	if err != nil {
		t.Fatal(err)
	}
	ended := sampleRecord("ao")
	ended.IsTerminated = true
	ended.Activity.State = domain.ActivityExited
	ended, err = s.CreateSession(ctx, ended)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	for _, id := range []domain.SessionID{live.ID, ended.ID, live.ID} {
		if err := s.MarkRemoteSpawned(ctx, id, now); err != nil {
			t.Fatalf("mark %s: %v", id, err)
		}
	}

	got, err := s.ListActiveRemoteSpawnedSessions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if want := []domain.SessionID{live.ID}; !reflect.DeepEqual(got, want) {
		t.Fatalf("active remote spawned = %v, want %v (local %s is unmarked)", got, want, local.ID)
	}
}
