package report

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

func mergeAnnouncementFixture(t *testing.T, ownerSet bool) (*Coordinator, *coordinatorStore, *coordinatorDelivery) {
	t.Helper()
	store := newCoordinatorStore(time.Now())
	owner := domain.SessionID("coordinator")
	var ownerPtr *domain.SessionID
	if ownerSet {
		ownerPtr = &owner
	}
	store.sessions = []domain.SessionRecord{
		{ID: owner, ProjectID: "p", Kind: domain.KindCoordinator},
		{ID: "worker", ProjectID: "p", Kind: domain.KindWorker, OwnerSessionID: ownerPtr},
	}
	delivery := &coordinatorDelivery{}
	return NewCoordinator(CoordinatorDeps{Store: store, Delivery: delivery}), store, delivery
}

func TestAnnounceMergedNoticesTheOwnerOnce(t *testing.T) {
	c, _, delivery := mergeAnnouncementFixture(t, true)
	ctx := context.Background()

	if err := c.AnnounceMerged(ctx, "worker", "https://example/pr/201", 201, "merge-commit"); err != nil {
		t.Fatal(err)
	}
	if err := c.AnnounceMerged(ctx, "worker", "https://example/pr/201", 201, "merge-commit"); err != nil {
		t.Fatal(err)
	}
	if len(delivery.submissions) != 1 {
		t.Fatalf("submissions = %+v, want one", delivery.submissions)
	}
	got := delivery.submissions[0]
	if got.target != "coordinator" || !strings.Contains(got.message, "PR #201") ||
		!strings.Contains(got.message, "merge-commit") || !strings.Contains(got.message, "worker") {
		t.Fatalf("notice = %+v", got)
	}
}

func TestTickCompletionAfterObserverNoticeIsNotDuplicated(t *testing.T) {
	c, _, delivery := mergeAnnouncementFixture(t, true)
	ctx := context.Background()

	if err := c.AnnounceMerged(ctx, "worker", "https://example/pr/201", 201, "merge-commit"); err != nil {
		t.Fatal(err)
	}
	tick := CompletionInput{SessionID: "worker", ActionKey: "merge:201@head", Kind: "merge",
		PRs: []CompletionPR{{Number: 201, MergeCommitSHA: "merge-commit"}}}
	if err := c.DeliverCompletion(ctx, tick); err != nil {
		t.Fatal(err)
	}
	if len(delivery.submissions) != 1 {
		t.Fatalf("tick repeated the observer notice: %+v", delivery.submissions)
	}
}

func TestObserverNoticeAfterTickCompletionIsNotDuplicated(t *testing.T) {
	c, _, delivery := mergeAnnouncementFixture(t, true)
	ctx := context.Background()

	tick := CompletionInput{SessionID: "worker", ActionKey: "merge:201@head", Kind: "merge",
		PRs: []CompletionPR{{Number: 201, MergeCommitSHA: "merge-commit"}}}
	if err := c.DeliverCompletion(ctx, tick); err != nil {
		t.Fatal(err)
	}
	if err := c.AnnounceMerged(ctx, "worker", "https://example/pr/201", 201, "merge-commit"); err != nil {
		t.Fatal(err)
	}
	if len(delivery.submissions) != 1 {
		t.Fatalf("observer repeated the tick notice: %+v", delivery.submissions)
	}
}

func TestAnnounceMergedSkipsRootOwnedAndTerminatedOwners(t *testing.T) {
	c, store, delivery := mergeAnnouncementFixture(t, false)
	ctx := context.Background()
	if err := c.AnnounceMerged(ctx, "worker", "https://example/pr/201", 201, "merge-commit"); err != nil {
		t.Fatal(err)
	}
	if len(delivery.submissions) != 0 {
		t.Fatalf("root-owned worker notified: %+v", delivery.submissions)
	}

	owner := domain.SessionID("coordinator")
	store.sessions[1].OwnerSessionID = &owner
	store.sessions[0].IsTerminated = true
	if err := c.AnnounceMerged(ctx, "worker", "https://example/pr/202", 202, "merge-commit-2"); err != nil {
		t.Fatal(err)
	}
	if len(delivery.submissions) != 0 {
		t.Fatalf("terminated owner notified: %+v", delivery.submissions)
	}
}

func TestAnnounceMergedReleasesClaimOnFailureSoItRetries(t *testing.T) {
	c, _, delivery := mergeAnnouncementFixture(t, true)
	ctx := context.Background()
	delivery.submitErr = ErrRecipientUnavailable
	if err := c.AnnounceMerged(ctx, "worker", "https://example/pr/201", 201, "merge-commit"); err == nil {
		t.Fatal("delivery failure was not returned")
	}
	delivery.submitErr = nil
	if err := c.AnnounceMerged(ctx, "worker", "https://example/pr/201", 201, "merge-commit"); err != nil {
		t.Fatal(err)
	}
	if len(delivery.submissions) != 1 {
		t.Fatalf("released claim did not retry: %+v", delivery.submissions)
	}
}

func TestAnnounceMergedWithoutAMergeCommitIsNotDropped(t *testing.T) {
	c, _, delivery := mergeAnnouncementFixture(t, true)
	ctx := context.Background()

	// A fast-forward or null mergeCommit reports no SHA; the notice must still go out.
	if err := c.AnnounceMerged(ctx, "worker", "https://example/pr/203", 203, ""); err != nil {
		t.Fatal(err)
	}
	if len(delivery.submissions) != 1 {
		t.Fatalf("empty-SHA merge dropped: %+v", delivery.submissions)
	}
	if got := delivery.submissions[0].message; !strings.Contains(got, "PR #203") || !strings.Contains(got, "no merge commit reported") {
		t.Fatalf("notice = %q", got)
	}
	if err := c.AnnounceMerged(ctx, "worker", "https://example/pr/203", 203, ""); err != nil {
		t.Fatal(err)
	}
	if len(delivery.submissions) != 1 {
		t.Fatalf("empty-SHA merge announced twice: %+v", delivery.submissions)
	}
}
