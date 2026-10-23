package session

import (
	"context"
	"errors"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/apierr"
)

func TestProjectIssueReadsFromTheProjectTracker(t *testing.T) {
	tests := []struct {
		name       string
		origin     string
		wantSCM    bool
		wantProv   domain.TrackerProvider
		wantNative string
	}{
		{name: "github origin", origin: "https://github.com/acme/repo.git", wantProv: domain.TrackerProviderGitHub, wantNative: "acme/repo#42"},
		{name: "gitlab origin", origin: "https://gitlab.com/acme/repo.git", wantSCM: true, wantProv: domain.TrackerProviderGitLab, wantNative: "acme/repo#42"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			st := newFakeStore()
			st.projects["mer"] = domain.ProjectRecord{ID: "mer", RepoOriginURL: tc.origin}
			tracker := &fakeTracker{issue: domain.Issue{Title: "Load me", Body: "## Acceptance criteria\n- ok"}}
			deps := Deps{Store: st, Tracker: tracker}
			if tc.wantSCM {
				deps.SCM = fakeSCM{}
			}
			svc := NewWithDeps(deps)

			issue, err := svc.ProjectIssue(context.Background(), "mer", 42)
			if err != nil {
				t.Fatalf("ProjectIssue: %v", err)
			}
			if issue.Title != "Load me" {
				t.Fatalf("issue = %+v", issue)
			}
			if len(tracker.ids) != 1 || tracker.ids[0].Provider != tc.wantProv || tracker.ids[0].Native != tc.wantNative {
				t.Fatalf("tracker ids = %+v, want %s %s", tracker.ids, tc.wantProv, tc.wantNative)
			}
		})
	}
}

func TestProjectIssueRejectsBadInput(t *testing.T) {
	st := newFakeStore()
	st.projects["mer"] = domain.ProjectRecord{ID: "mer", RepoOriginURL: "https://github.com/acme/repo.git"}

	tracker := &fakeTracker{}
	svc := NewWithDeps(Deps{Store: st, Tracker: tracker})
	if _, err := svc.ProjectIssue(context.Background(), "mer", 0); !hasAPICode(err, "INVALID_ISSUE_NUMBER") {
		t.Fatalf("number 0 err = %v, want INVALID_ISSUE_NUMBER", err)
	}
	if _, err := svc.ProjectIssue(context.Background(), "missing", 1); !hasAPICode(err, "PROJECT_NOT_FOUND") {
		t.Fatalf("unknown project err = %v, want PROJECT_NOT_FOUND", err)
	}
	if len(tracker.ids) != 0 {
		t.Fatalf("tracker was called for rejected input: %+v", tracker.ids)
	}

	noTracker := NewWithDeps(Deps{Store: st})
	if _, err := noTracker.ProjectIssue(context.Background(), "mer", 1); !hasAPICode(err, "TRACKER_UNAVAILABLE") {
		t.Fatalf("no tracker err = %v, want TRACKER_UNAVAILABLE", err)
	}
}

func hasAPICode(err error, code string) bool {
	var apiErr *apierr.Error
	return errors.As(err, &apiErr) && apiErr.Code == code
}
