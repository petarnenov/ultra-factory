package scm

import (
	"context"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	reviewcore "github.com/aoagents/agent-orchestrator/backend/internal/review"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/sqlitetest"
)

// Each host allocates project numbers from its own database. Before ids were
// host-scoped both produced "p-1", so one pull request matched both.
func TestPoll_CrossHostSessionIDsDoNotSharePRs(t *testing.T) {
	recA, recB := crossHostSessions(t)
	if recA.ID == recB.ID {
		t.Fatalf("hosts share session id %q", recA.ID)
	}

	sourceBranch := "ao/" + string(recA.ID) + "/orchestrator-chat-kill"
	for _, tc := range []struct {
		name    string
		session domain.SessionRecord
		want    bool
	}{
		{name: "owning host", session: recA, want: true},
		{name: "other host", session: recB, want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &fakeStore{
				sessions: []domain.SessionRecord{tc.session},
				projects: map[string]domain.ProjectRecord{"p": {ID: "p", RepoOriginURL: "https://github.com/o/r.git"}},
				prs:      map[domain.SessionID][]domain.PullRequest{},
				checks:   map[string][]domain.PullRequestCheck{},
			}
			prObs := testObs(280)
			prObs.PR.SourceBranch = sourceBranch
			provider := &fakeProvider{
				repoGuards: map[string]ports.SCMGuardResult{prKey(testRepo, 0): {ETag: "repo-v1"}},
				openPRs: map[string][]ports.SCMPRObservation{prKey(testRepo, 0): {
					{URL: "https://github.com/o/r/pull/280", Number: 280, SourceBranch: sourceBranch, HeadRepo: "o/r", TargetBranch: "main", HeadSHA: "sha280"},
				}},
				observations: map[string]ports.SCMObservation{prKey(testRepo, 280): prObs},
			}
			obs := newTestObserver(store, provider, &fakeLifecycle{}, time.Unix(1, 0).UTC())
			if err := obs.Poll(context.Background()); err != nil {
				t.Fatal(err)
			}

			if !tc.want {
				if len(store.writes) != 0 {
					t.Fatalf("other host claimed %d PR(s): %+v", len(store.writes), store.writes)
				}
				prs, err := store.ListPRsBySession(context.Background(), tc.session.ID)
				if err != nil || len(prs) != 0 {
					t.Fatalf("session-to-PR view = %+v err=%v", prs, err)
				}
				if reviews := reviewcore.Plan(prs, nil); len(reviews) != 0 {
					t.Fatalf("review registry = %+v", reviews)
				}
				return
			}

			if len(store.writes) == 0 {
				t.Fatalf("owning host did not record the PR")
			}
			for _, write := range store.writes {
				if write.pr.SessionID != tc.session.ID {
					t.Fatalf("owning host wrote PR for %q, want %q", write.pr.SessionID, tc.session.ID)
				}
			}
			last := store.writes[len(store.writes)-1].pr
			if reviews := reviewcore.Plan([]domain.PullRequest{last}, nil); len(reviews) != 1 {
				t.Fatalf("review registry = %+v", reviews)
			}
		})
	}
}

// Cross-host sessions are created through the real allocator so the test fails
// if ids stop carrying the host token.
func crossHostSessions(t *testing.T) (domain.SessionRecord, domain.SessionRecord) {
	t.Helper()
	hostA := sqlitetest.MustOpen(t)
	hostB := sqlitetest.MustOpen(t)
	hostA.SetSessionIDHostToken("haaa111")
	hostB.SetSessionIDHostToken("hbbb222")
	return createHostSession(t, hostA, "haaa111"), createHostSession(t, hostB, "hbbb222")
}

func createHostSession(t *testing.T, st *sqlite.Store, hostToken string) domain.SessionRecord {
	t.Helper()
	ctx := context.Background()
	if err := st.UpsertProject(ctx, domain.ProjectRecord{
		ID: "p", Path: t.TempDir(), RegisteredAt: time.Unix(1, 0).UTC(),
		RepoOriginURL: "https://github.com/o/r.git",
	}); err != nil {
		t.Fatalf("seed project: %v", err)
	}
	now := time.Unix(1, 0).UTC()
	rec, err := st.CreateSession(ctx, domain.SessionRecord{
		ProjectID: "p",
		Kind:      domain.KindWorker,
		Harness:   domain.HarnessClaudeCode,
		Activity:  domain.Activity{State: domain.ActivityActive, LastActivityAt: now},
		CreatedAt: now,
		UpdatedAt: now,
	})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if want := "p-" + hostToken + "-1"; string(rec.ID) != want {
		t.Fatalf("session id = %q, want %q", rec.ID, want)
	}
	rec.Metadata.Branch = "ao/" + string(rec.ID) + "/root"
	return rec
}

// A workspace session predating host-scoped ids owns bare "ao/<id>" hyphen
// siblings. The host token sits before the counter so a scoped id is not one.
func TestMatchSession_LegacyWorkspaceBranchKeepsItsOwnSiblingsOnly(t *testing.T) {
	legacy := sessionRepo{
		session:   domain.SessionRecord{ID: "p-1", ProjectID: "p"},
		branch:    "ao/p-1",
		workspace: true,
	}
	if _, ok := matchSession([]sessionRepo{legacy}, "ao/p-1-billing"); !ok {
		t.Fatal("legacy session lost ownership of its own hyphen sibling")
	}
	if got, ok := matchSession([]sessionRepo{legacy}, "ao/p-haaa111-1-billing"); ok {
		t.Fatalf("legacy session %q claimed a scoped session branch", got.session.ID)
	}
}
