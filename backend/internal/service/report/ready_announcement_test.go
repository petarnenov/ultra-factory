package report

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

const readyPRURL = "https://github.com/acme/app/pull/7"

type readyStore struct {
	sessions  []domain.SessionRecord
	project   domain.ProjectRecord
	prs       []domain.PullRequest
	comments  []domain.PullRequestComment
	runs      []domain.ReviewRun
	claims    map[string]bool
	reports   []domain.ReportRecord
	createErr error
}

func newReadyStore(owner *domain.SessionID) *readyStore {
	return &readyStore{
		sessions: []domain.SessionRecord{
			{ID: "root", ProjectID: "p", Kind: domain.KindOrchestrator},
			{ID: "coordinator", ProjectID: "p", Kind: domain.KindCoordinator},
			{ID: "worker", ProjectID: "p", Kind: domain.KindWorker, OwnerSessionID: owner},
		},
		project: domain.ProjectRecord{ID: "p"},
		prs:     []domain.PullRequest{readyPR("abc")},
		claims:  map[string]bool{},
	}
}

func readyPR(head string) domain.PullRequest {
	return domain.PullRequest{URL: readyPRURL, Number: 7, HeadSHA: head,
		CI: domain.CIPassing, Review: domain.ReviewApproved, Mergeability: domain.MergeMergeable}
}

func (s *readyStore) ListSessions(context.Context, domain.ProjectID) ([]domain.SessionRecord, error) {
	return s.sessions, nil
}

func (s *readyStore) GetSession(_ context.Context, id domain.SessionID) (domain.SessionRecord, bool, error) {
	for _, rec := range s.sessions {
		if rec.ID == id {
			return rec, true, nil
		}
	}
	return domain.SessionRecord{}, false, nil
}

func (s *readyStore) GetProject(context.Context, string) (domain.ProjectRecord, bool, error) {
	return s.project, true, nil
}

func (s *readyStore) ListPRsBySession(context.Context, domain.SessionID) ([]domain.PullRequest, error) {
	return s.prs, nil
}

func (s *readyStore) ListPRComments(context.Context, string) ([]domain.PullRequestComment, error) {
	return s.comments, nil
}

func (s *readyStore) ListReviewRunsBySession(context.Context, domain.SessionID) ([]domain.ReviewRun, error) {
	return s.runs, nil
}

func (s *readyStore) ClaimPRMergeAnnouncement(_ context.Context, key, _ string, _ domain.SessionID, _ time.Time) (bool, error) {
	if s.claims[key] {
		return false, nil
	}
	s.claims[key] = true
	return true, nil
}

func (s *readyStore) ReleasePRMergeAnnouncement(_ context.Context, key string) error {
	delete(s.claims, key)
	return nil
}

func (s *readyStore) CreateReport(_ context.Context, r domain.ReportRecord) (domain.ReportRecord, error) {
	if s.createErr != nil {
		return domain.ReportRecord{}, s.createErr
	}
	s.reports = append(s.reports, r)
	return r, nil
}

func newReadyAnnouncer(store *readyStore) *ReadyAnnouncer {
	return NewReadyAnnouncer(store, New(Deps{Store: store}))
}

func announce(t *testing.T, a *ReadyAnnouncer) {
	t.Helper()
	if err := a.AnnounceReadyToMerge(context.Background(), "worker"); err != nil {
		t.Fatal(err)
	}
}

func onlyReport(t *testing.T, store *readyStore) domain.ReportRecord {
	t.Helper()
	if len(store.reports) != 1 {
		t.Fatalf("reports = %+v, want one", store.reports)
	}
	return store.reports[0]
}

func TestReadyToMergeTellsTheRootToMergeOncePerHead(t *testing.T) {
	store := newReadyStore(nil)
	a := newReadyAnnouncer(store)

	announce(t, a)
	announce(t, a)

	got := onlyReport(t, store)
	if got.SessionID != "worker" || got.State != domain.ReportCheckpoint || got.AvailableAt.After(got.CreatedAt) {
		t.Fatalf("report = %+v, want a worker checkpoint due now", got)
	}
	if !strings.Contains(got.Note, "gh pr merge 7 --squash --match-head-commit abc") {
		t.Fatalf("note = %q, want the merge command", got.Note)
	}
}

func TestReadyToMergeAsksTheRootToTellTheHumanWhenAutoMergeIsOff(t *testing.T) {
	store := newReadyStore(nil)
	off := false
	store.project.Config.OrchestratorAutoMerge = &off

	announce(t, newReadyAnnouncer(store))

	note := onlyReport(t, store).Note
	if !strings.Contains(note, "tell the human") || strings.Contains(note, "gh pr merge") {
		t.Fatalf("note = %q, want a hand-off to the human", note)
	}
}

func TestReadyToMergeWaitsForAnApprovedReviewBeforeWakingTheCoordinator(t *testing.T) {
	owner := domain.SessionID("coordinator")
	store := newReadyStore(&owner)
	a := newReadyAnnouncer(store)

	announce(t, a)
	if len(store.reports) != 0 {
		t.Fatalf("reports = %+v, want none before an AO approval", store.reports)
	}

	store.runs = []domain.ReviewRun{{SessionID: "worker", PRURL: readyPRURL, TargetSHA: "abc",
		Status: domain.ReviewRunComplete, Verdict: domain.VerdictApproved}}
	announce(t, a)

	if note := onlyReport(t, store).Note; !strings.Contains(note, "ao coordinator report --ready-pr 7@abc") {
		t.Fatalf("note = %q, want the coordinator hand-off", note)
	}
}

func TestReadyToMergeIgnoresAnApprovalOfAnotherHead(t *testing.T) {
	store := newReadyStore(nil)
	store.sessions[2].AutoReviewEnabled = true
	store.runs = []domain.ReviewRun{{SessionID: "worker", PRURL: readyPRURL, TargetSHA: "old",
		Status: domain.ReviewRunComplete, Verdict: domain.VerdictApproved}}

	announce(t, newReadyAnnouncer(store))

	if len(store.reports) != 0 {
		t.Fatalf("reports = %+v, want none for a stale approval", store.reports)
	}
}

func TestReadyToMergeSkipsPRsThatAreNotReady(t *testing.T) {
	tests := map[string]func(*readyStore){
		"ci pending":  func(s *readyStore) { s.prs[0].CI = domain.CIPending },
		"conflicting": func(s *readyStore) { s.prs[0].Mergeability = domain.MergeConflicting },
		"draft":       func(s *readyStore) { s.prs[0].Draft = true },
		"no head":     func(s *readyStore) { s.prs[0].HeadSHA = "" },
		"unresolved comment": func(s *readyStore) {
			s.comments = []domain.PullRequestComment{{File: "main.go", Line: 3}}
		},
		"terminated worker": func(s *readyStore) { s.sessions[2].IsTerminated = true },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			store := newReadyStore(nil)
			mutate(store)

			announce(t, newReadyAnnouncer(store))

			if len(store.reports) != 0 {
				t.Fatalf("reports = %+v, want none", store.reports)
			}
		})
	}
}

func TestReadyToMergeAnnouncesANewHeadAgain(t *testing.T) {
	store := newReadyStore(nil)
	a := newReadyAnnouncer(store)

	announce(t, a)
	store.prs[0] = readyPR("def")
	announce(t, a)

	if len(store.reports) != 2 || !strings.Contains(store.reports[1].Note, "def") {
		t.Fatalf("reports = %+v, want a second report for head def", store.reports)
	}
}

func TestReadyToMergeReleasesTheClaimWhenTheReportFails(t *testing.T) {
	store := newReadyStore(nil)
	store.createErr = errors.New("disk full")
	a := newReadyAnnouncer(store)

	if err := a.AnnounceReadyToMerge(context.Background(), "worker"); err == nil {
		t.Fatal("want the report error")
	}
	store.createErr = nil
	announce(t, a)

	onlyReport(t, store)
}
