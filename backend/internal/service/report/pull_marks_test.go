package report

import (
	"context"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/requestscope"
)

type markingPullStore struct {
	pullStore
	marked []domain.SessionID
}

func (m *markingPullStore) MarkRemoteSpawned(_ context.Context, id domain.SessionID, _ time.Time) error {
	m.marked = append(m.marked, id)
	return nil
}

func TestPeerPullMarksTheWorkerAsRemotelyStarted(t *testing.T) {
	st := &markingPullStore{pullStore: pullStore{fakeStore: fakeStore{ok: true, session: domain.SessionRecord{ID: "w", Kind: domain.KindWorker}}}}
	svc := New(Deps{Store: st})

	if _, _, err := svc.ListDuePending(requestscope.WithLAN(context.Background()), "w"); err != nil {
		t.Fatal(err)
	}
	if len(st.marked) != 1 || st.marked[0] != "w" {
		t.Fatalf("marked = %v, want w after a pull over the LAN listener", st.marked)
	}
}

func TestLoopbackPullDoesNotMarkTheWorker(t *testing.T) {
	st := &markingPullStore{pullStore: pullStore{fakeStore: fakeStore{ok: true, session: domain.SessionRecord{ID: "w", Kind: domain.KindWorker}}}}
	svc := New(Deps{Store: st})

	if _, _, err := svc.ListDuePending(context.Background(), "w"); err != nil {
		t.Fatal(err)
	}
	if len(st.marked) != 0 {
		t.Fatalf("marked = %v, want none for a loopback read", st.marked)
	}
}
