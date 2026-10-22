package report

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

func TestCompletionResolvesLocalCoordinatorOwnerAndStableAcceptanceKey(t *testing.T) {
	for _, remote := range []bool{false, true} {
		t.Run(map[bool]string{false: "local", true: "remote"}[remote], func(t *testing.T) {
			store := newCoordinatorStore(time.Now())
			owner := domain.SessionID("coordinator")
			store.sessions = []domain.SessionRecord{
				{ID: owner, ProjectID: "p", Kind: domain.KindCoordinator},
				{ID: "worker", ProjectID: "p", Kind: domain.KindWorker, OwnerSessionID: &owner},
			}
			input := CompletionInput{SessionID: "worker", ActionKey: "merge:201@head", Kind: "merge", PRs: []CompletionPR{{Number: 201, MergeCommitSHA: "merge-commit"}}}
			if remote {
				input.HostID = "remote-host"
				store.links = map[string]domain.RemoteWorkerLink{"remote-host/worker": {ProjectID: "p", OwnerSessionID: &owner}}
			}
			delivery := &coordinatorDelivery{}
			c := NewCoordinator(CoordinatorDeps{Store: store, Delivery: delivery})

			if err := c.DeliverCompletion(context.Background(), input); err != nil {
				t.Fatal(err)
			}
			if len(delivery.submissions) != 1 {
				t.Fatalf("a repeated completion for a merge already announced was sent: %+v", delivery.submissions)
			}
			got := delivery.submissions[0]
			if key, ok := domain.ReportDeliveryID(domain.WrapReportDelivery(got.key, got.message)); !ok || key != got.key {
				t.Fatalf("notification key cannot be accepted by report hooks: %q", got.key)
			}
			if got.target != owner || !strings.Contains(got.message, "PR #201 merge commit merge-commit") || !strings.Contains(got.message, "worker") {
				t.Fatalf("notification: %+v", got)
			}
			store.sessions[0].IsTerminated = true
			next := input
			next.PRs = []CompletionPR{{Number: 202, MergeCommitSHA: "merge-commit-2"}}
			if err := c.DeliverCompletion(context.Background(), next); err != nil {
				t.Fatal(err)
			}
			if len(delivery.submissions) != 1 {
				t.Fatal("notified terminated owner")
			}
		})
	}
}

func TestCompletionSkipsRootAndRejectsInvalidEvents(t *testing.T) {
	store := newCoordinatorStore(time.Now())
	store.sessions = []domain.SessionRecord{{ID: "worker", ProjectID: "p", Kind: domain.KindWorker}}
	delivery := &coordinatorDelivery{}
	c := NewCoordinator(CoordinatorDeps{Store: store, Delivery: delivery})
	if err := c.DeliverCompletion(context.Background(), CompletionInput{}); err == nil {
		t.Fatal("accepted invalid event")
	}
	input := CompletionInput{SessionID: "worker", ActionKey: "merge:1@head", Kind: "merge", PRs: []CompletionPR{{Number: 1, MergeCommitSHA: "commit"}}}
	if err := c.DeliverCompletion(context.Background(), input); err != nil {
		t.Fatal(err)
	}
	if len(delivery.submissions) != 0 {
		t.Fatal("notified a root-owned worker")
	}
}

func TestCompletionOnlyNotifiesLiveSameProjectCoordinators(t *testing.T) {
	for _, tc := range []struct {
		name    string
		kind    domain.SessionKind
		project domain.ProjectID
		exited  bool
	}{
		{"root", domain.KindOrchestrator, "p", false},
		{"worker", domain.KindWorker, "p", false},
		{"other project", domain.KindCoordinator, "other", false},
		{"exited", domain.KindCoordinator, "p", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := newCoordinatorStore(time.Now())
			owner := domain.SessionID("owner")
			recipient := domain.SessionRecord{ID: owner, ProjectID: tc.project, Kind: tc.kind}
			if tc.exited {
				recipient.Activity.State = domain.ActivityExited
			}
			store.sessions = []domain.SessionRecord{recipient, {ID: "worker", ProjectID: "p", Kind: domain.KindWorker, OwnerSessionID: &owner}}
			delivery := &coordinatorDelivery{}
			c := NewCoordinator(CoordinatorDeps{Store: store, Delivery: delivery})
			if err := c.DeliverCompletion(context.Background(), CompletionInput{SessionID: "worker", ActionKey: "close", Kind: "close", PRs: []CompletionPR{{Number: 1, MergeCommitSHA: "commit"}}}); err != nil {
				t.Fatal(err)
			}
			if len(delivery.submissions) != 0 {
				t.Fatal("notified invalid owner")
			}
		})
	}
}

func TestCompletionConsumesSemanticTimeoutAndRetriesOtherErrors(t *testing.T) {
	store := newCoordinatorStore(time.Now())
	owner := domain.SessionID("coordinator")
	store.sessions = []domain.SessionRecord{
		{ID: owner, ProjectID: "p", Kind: domain.KindCoordinator},
		{ID: "worker", ProjectID: "p", Kind: domain.KindWorker, OwnerSessionID: &owner},
	}
	delivery := &coordinatorDelivery{submitErr: domain.ErrSemanticAcceptanceTimeout}
	c := NewCoordinator(CoordinatorDeps{Store: store, Delivery: delivery})
	input := CompletionInput{SessionID: "worker", ActionKey: "merge:1@head", Kind: "merge", PRs: []CompletionPR{{Number: 1, MergeCommitSHA: "commit"}}}
	if err := c.DeliverCompletion(context.Background(), input); err != nil {
		t.Fatalf("semantic timeout should be consumed: %v", err)
	}
	delivery.submitErr = ErrRecipientUnavailable
	retry := CompletionInput{SessionID: "worker", ActionKey: "merge:2@head", Kind: "merge", PRs: []CompletionPR{{Number: 2, MergeCommitSHA: "commit-2"}}}
	if err := c.DeliverCompletion(context.Background(), retry); err == nil {
		t.Fatal("delivery failure was not returned for retry")
	}
	delivery.submitErr = nil
	if err := c.DeliverCompletion(context.Background(), retry); err != nil {
		t.Fatalf("retry after a failed send: %v", err)
	}
	// The failed attempt is not recorded, so the one submission is the retry:
	// the claim was released and the retry was not dropped as a duplicate.
	if n := len(delivery.submissions); n != 1 || !strings.Contains(delivery.submissions[0].message, "commit-2") {
		t.Fatalf("failed claim was not released for retry: %+v", delivery.submissions)
	}
}

func TestCompletionOwnerSnapshotSurvivesRemoteLinkRemoval(t *testing.T) {
	store := newCoordinatorStore(time.Now())
	store.sessions = []domain.SessionRecord{{ID: "coordinator", ProjectID: "p", Kind: domain.KindCoordinator}}
	delivery := &coordinatorDelivery{}
	c := NewCoordinator(CoordinatorDeps{Store: store, Delivery: delivery})
	input := CompletionInput{HostID: "host", SessionID: "closed-worker", ActionKey: "close", Kind: "close",
		Recipient: &CompletionRecipient{OwnerSessionID: "coordinator", ProjectID: "p"},
		PRs:       []CompletionPR{{Number: 201, MergeCommitSHA: "commit"}},
	}
	if err := c.DeliverCompletion(context.Background(), input); err != nil {
		t.Fatal(err)
	}
	if len(delivery.submissions) != 1 || delivery.submissions[0].target != "coordinator" {
		t.Fatalf("lost close notification: %+v", delivery.submissions)
	}
}

func TestCompletionClaimsNothingWhenAnyPRIsInvalid(t *testing.T) {
	store := newCoordinatorStore(time.Now())
	owner := domain.SessionID("coordinator")
	store.sessions = []domain.SessionRecord{
		{ID: owner, ProjectID: "p", Kind: domain.KindCoordinator},
		{ID: "worker", ProjectID: "p", Kind: domain.KindWorker, OwnerSessionID: &owner},
	}
	delivery := &coordinatorDelivery{}
	c := NewCoordinator(CoordinatorDeps{Store: store, Delivery: delivery})
	ctx := context.Background()

	bad := CompletionInput{SessionID: "worker", ActionKey: "merge:1-2@head", Kind: "merge",
		PRs: []CompletionPR{{Number: 1, MergeCommitSHA: "sha-a"}, {Number: 2}}}
	if err := c.DeliverCompletion(ctx, bad); err == nil {
		t.Fatal("invalid PR was accepted")
	}
	if len(store.merges) != 0 {
		t.Fatalf("invalid completion left claims: %v", store.merges)
	}
	// The observer can still announce sha-a, because nothing was claimed.
	if err := c.AnnounceMerged(ctx, "worker", "https://example/pr/1", 1, "sha-a"); err != nil {
		t.Fatal(err)
	}
	if len(delivery.submissions) != 1 {
		t.Fatalf("observer notice blocked by a leaked claim: %+v", delivery.submissions)
	}
}

func TestCompletionReleasesEarlierClaimsWhenALaterClaimFails(t *testing.T) {
	store := newCoordinatorStore(time.Now())
	owner := domain.SessionID("coordinator")
	store.sessions = []domain.SessionRecord{
		{ID: owner, ProjectID: "p", Kind: domain.KindCoordinator},
		{ID: "worker", ProjectID: "p", Kind: domain.KindWorker, OwnerSessionID: &owner},
	}
	store.claimErrSHA = "sha-b"
	store.claimErr = errors.New("database locked")
	delivery := &coordinatorDelivery{}
	c := NewCoordinator(CoordinatorDeps{Store: store, Delivery: delivery})
	ctx := context.Background()

	in := CompletionInput{SessionID: "worker", ActionKey: "merge:1-2@head", Kind: "merge",
		PRs: []CompletionPR{{Number: 1, MergeCommitSHA: "sha-a"}, {Number: 2, MergeCommitSHA: "sha-b"}}}
	if err := c.DeliverCompletion(ctx, in); err == nil {
		t.Fatal("claim failure was not returned")
	}
	if len(store.merges) != 0 {
		t.Fatalf("claim failure left an earlier claim behind: %v", store.merges)
	}
	if err := c.AnnounceMerged(ctx, "worker", "https://example/pr/1", 1, "sha-a"); err != nil {
		t.Fatal(err)
	}
	if len(delivery.submissions) != 1 {
		t.Fatalf("earlier merge never announced: %+v", delivery.submissions)
	}
}
