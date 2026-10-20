package report

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

// countingStore counts the session reads a report pass makes.
type countingStore struct {
	*coordinatorStore
	lists int
	gets  map[domain.SessionID]int
}

func (s *countingStore) ListSessions(ctx context.Context, project domain.ProjectID) ([]domain.SessionRecord, error) {
	s.lists++
	var out []domain.SessionRecord
	all, _ := s.coordinatorStore.ListSessions(ctx, project)
	for _, rec := range all {
		if rec.ProjectID == project {
			out = append(out, rec)
		}
	}
	return out, nil
}

func (s *countingStore) GetSession(ctx context.Context, id domain.SessionID) (domain.SessionRecord, bool, error) {
	s.gets[id]++
	return s.coordinatorStore.GetSession(ctx, id)
}

func TestReportPassReadsSessionsOncePerProjectAndWorker(t *testing.T) {
	now := time.Date(2026, 10, 20, 10, 0, 0, 0, time.UTC)
	base := newCoordinatorStore(now)
	const projects, workersPerProject, reportsPerWorker = 3, 10, 7 // 210 reports
	for p := 0; p < projects; p++ {
		project := domain.ProjectID(fmt.Sprintf("p%d", p))
		coord := domain.SessionID(fmt.Sprintf("coord-%d", p))
		base.sessions = append(base.sessions,
			domain.SessionRecord{ID: domain.SessionID(fmt.Sprintf("root-%d", p)), ProjectID: project, Kind: domain.KindOrchestrator, CreatedAt: now},
			domain.SessionRecord{ID: coord, ProjectID: project, Kind: domain.KindCoordinator, CoordinatorScope: "s", CreatedAt: now})
		for i := 0; i < 100; i++ { // terminated history the listing still returns
			base.sessions = append(base.sessions, domain.SessionRecord{ID: domain.SessionID(fmt.Sprintf("old-%d-%d", p, i)), ProjectID: project, Kind: domain.KindWorker, IsTerminated: true})
		}
		for w := 0; w < workersPerProject; w++ {
			worker := domain.SessionID(fmt.Sprintf("w-%d-%d", p, w))
			owner := coord
			if w%2 == 1 {
				owner = "gone" // stale owner: repaired to the root
			}
			base.sessions = append(base.sessions, domain.SessionRecord{ID: worker, ProjectID: project, Kind: domain.KindWorker, OwnerSessionID: &owner})
			for r := 0; r < reportsPerWorker; r++ {
				rec := reportAt(fmt.Sprintf("r-%d-%d-%d", p, w, r), worker, now, now, "n")
				rec.ProjectID = project
				base.add(rec)
			}
		}
	}
	store := &countingStore{coordinatorStore: base, gets: map[domain.SessionID]int{}}
	delivery := &coordinatorDelivery{}
	c := NewCoordinator(CoordinatorDeps{Store: store, Delivery: delivery, Now: func() time.Time { return now }})

	if _, err := c.runReportsLocked(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(delivery.submissions) == 0 {
		t.Fatal("nothing was delivered")
	}
	if store.lists > projects {
		t.Fatalf("ListSessions calls = %d, want at most one per project (%d)", store.lists, projects)
	}
	for id, n := range store.gets {
		if len(id) > 2 && id[:2] == "w-" && n > 2+reportsPerWorker {
			t.Fatalf("worker %s read %d times", id, n)
		}
	}
	for p := 0; p < projects; p++ {
		if n := store.gets[domain.SessionID(fmt.Sprintf("coord-%d", p))]; n > 1 {
			t.Fatalf("coordinator coord-%d read %d times, want once", p, n)
		}
	}
}
