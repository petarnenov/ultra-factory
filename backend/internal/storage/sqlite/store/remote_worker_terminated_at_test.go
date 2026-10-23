package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

func TestRemoteLinkKeepsTheFirstTerminatedReadUntilTheWorkerIsLive(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	seedProject(t, s, "ao")
	now := time.Now().UTC().Truncate(time.Second)
	link := domain.RemoteWorkerLink{HostID: "host-build", HostLabel: "build", RemoteSessionID: "w-1", RemoteProjectID: "widgets",
		ProjectID: "ao", DisplayName: "remote", CreatedAt: now}
	if err := s.UpsertRemoteWorkerLink(ctx, link); err != nil {
		t.Fatal(err)
	}
	read := func(at time.Time, state string) *time.Time {
		t.Helper()
		if err := s.UpdateRemoteWorkerLinkLiveness(ctx, "host-build", "w-1", at, state); err != nil {
			t.Fatal(err)
		}
		got, ok, err := s.GetRemoteWorkerLink(ctx, "host-build", "w-1")
		if err != nil || !ok {
			t.Fatalf("get link: ok=%v err=%v", ok, err)
		}
		return got.TerminatedAt
	}

	if got := read(now, domain.RemoteLinkLive); got != nil {
		t.Fatalf("live worker has terminated_at %v", got)
	}
	if got := read(now.Add(time.Minute), domain.RemoteLinkTerminated); got == nil || !got.Equal(now.Add(time.Minute)) {
		t.Fatalf("first terminated read = %v, want %v", got, now.Add(time.Minute))
	}
	if got := read(now.Add(time.Hour), domain.RemoteLinkTerminated); got == nil || !got.Equal(now.Add(time.Minute)) {
		t.Fatalf("later terminated read moved terminated_at to %v", got)
	}
	if got := read(now.Add(2*time.Hour), domain.RemoteLinkLive); got != nil {
		t.Fatalf("restored worker kept terminated_at %v", got)
	}
}
