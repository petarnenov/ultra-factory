package review

import (
	"context"
	"errors"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

func TestReviewReferenceHelpers(t *testing.T) {
	tests := []struct {
		name string
		pr   domain.PullRequest
		want ports.SCMPRRef
		bad  bool
	}{
		{name: "github inferred from URL", pr: domain.PullRequest{URL: "https://github.com/acme/widget/pull/17", Number: 17}, want: ports.SCMPRRef{Repo: ports.SCMRepo{Provider: "github", Host: "github.com", Repo: "acme/widget", Owner: "acme", Name: "widget"}, Number: 17, URL: "https://github.com/acme/widget/pull/17"}},
		{name: "gitlab repository split once", pr: domain.PullRequest{URL: "https://gitlab.example/acme/group/widget/-/merge_requests/2", Number: 2, Provider: "gitlab", Host: "gitlab.example", Repo: "acme/group/widget"}, want: ports.SCMPRRef{Repo: ports.SCMRepo{Provider: "gitlab", Host: "gitlab.example", Repo: "acme/group/widget", Owner: "acme", Name: "group/widget"}, Number: 2, URL: "https://gitlab.example/acme/group/widget/-/merge_requests/2"}},
		{name: "malformed pull request", pr: domain.PullRequest{URL: "https://github.com/acme/widget/pull/3"}, bad: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := reviewRequestRef(tt.pr)
			if tt.bad {
				if err == nil {
					t.Fatal("reviewRequestRef error = nil, want invalid reference")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Fatalf("reviewRequestRef = %#v, want %#v", got, tt.want)
			}
		})
	}
	for _, tc := range []struct {
		raw, provider, host string
		owner, repo         string
	}{
		{"https://gitlab.example/a/b/-/merge_requests/1", "gitlab", "gitlab.example", "", ""},
		{"https://github.com/a/b/pull/1", "github", "github.com", "a", "b"},
		{"https://github.com/a/b/pulls/1", "", "github.com", "", ""},
		{"://bad", "", "", "", ""},
	} {
		if got := providerFromPRURL(tc.raw); got != tc.provider {
			t.Errorf("providerFromPRURL(%q) = %q, want %q", tc.raw, got, tc.provider)
		}
		if got := hostFromPRURL(tc.raw); got != tc.host {
			t.Errorf("hostFromPRURL(%q) = %q, want %q", tc.raw, got, tc.host)
		}
		owner, repo := githubOwnerRepoFromPRURL(tc.raw)
		if owner != tc.owner || repo != tc.repo {
			t.Errorf("githubOwnerRepoFromPRURL(%q) = %q/%q, want %q/%q", tc.raw, owner, repo, tc.owner, tc.repo)
		}
	}
}

func TestRequestRereviewValidationAndCommentAuthor(t *testing.T) {
	ctx := context.Background()
	store := &fakeStore{prs: []domain.PullRequest{{URL: "https://github.com/acme/widget/pull/7", Number: 7, Provider: "github", Repo: "acme/widget"}}, prComments: map[string][]domain.PullRequestComment{
		"https://github.com/acme/widget/pull/7": {{Author: "@REVIEWER"}},
	}}
	requester := &fakeReviewRequester{}
	svc := New(nil, store, WithReviewRequester(requester))
	if err := svc.RequestRereview(ctx, "worker", "", "@reviewer"); err != nil {
		t.Fatalf("RequestRereview from matching comment author: %v", err)
	}
	if requester.request.Reviewer != "reviewer" || requester.request.PR.Number != 7 {
		t.Fatalf("request = %#v", requester.request)
	}

	for _, tc := range []struct {
		name        string
		worker      domain.SessionID
		reviewer    string
		want        error
		wantMessage string
	}{
		{"missing worker", "", "reviewer", ErrInvalid, "review: invalid input: worker session id is required"},
		{"missing reviewer", "worker", "@", ErrInvalid, "review: invalid input: reviewer is required"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := svc.RequestRereview(ctx, tc.worker, "", tc.reviewer)
			if !errors.Is(err, tc.want) {
				t.Fatalf("error = %v, want %v", err, tc.want)
			}
			if err.Error() != tc.wantMessage {
				t.Fatalf("error message = %q, want %q", err, tc.wantMessage)
			}
		})
	}
}

func TestSelectRereviewPRRequiresExplicitChoiceWhenAmbiguous(t *testing.T) {
	prs := []domain.PullRequest{{URL: "https://host/one", HTMLURL: "https://host/one/html"}, {URL: "https://host/two", HTMLURL: "https://host/two/html"}}
	if _, ok := selectRereviewPR(prs, ""); ok {
		t.Fatal("empty URL selected one of multiple PRs")
	}
	got, ok := selectRereviewPR(prs, "https://host/two/html")
	if !ok || got.URL != "https://host/two" {
		t.Fatalf("HTML URL selection = %#v, %v", got, ok)
	}
}
