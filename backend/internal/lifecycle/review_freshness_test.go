package lifecycle

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

// scriptedReviewThreads is a provider stand-in whose thread state a test sets
// between observations, e.g. resolved on the provider but stale in the rows.
type scriptedReviewThreads struct {
	threads []ports.SCMReviewThreadObservation
	err     error
	calls   int
}

func (f *scriptedReviewThreads) FetchPRReviewThreads(context.Context, domain.PullRequest) (ports.SCMReviewObservation, error) {
	f.calls++
	if f.err != nil {
		return ports.SCMReviewObservation{}, f.err
	}
	return ports.SCMReviewObservation{Threads: f.threads}, nil
}

var reviewFreshnessNow = time.Date(2026, 10, 22, 13, 11, 50, 0, time.UTC)

func reviewFreshnessFixture(t *testing.T, rec domain.SessionRecord) (*Manager, *fakeStore, *fakeMessenger, *scriptedReviewThreads) {
	t.Helper()
	m, st, msg := newManager()
	fetcher := &scriptedReviewThreads{threads: []ports.SCMReviewThreadObservation{{ID: "T1"}}}
	m.SetReviewThreadFetcher(fetcher)
	m.clock = func() time.Time { return reviewFreshnessNow }
	st.sessions["mer-1"] = rec
	st.comments["pr1"] = []domain.PullRequestComment{
		{ID: "1", ThreadID: "T1", Author: "alice", File: "remotes-main.ts", Line: 96, Body: "fix this", AutoInjectReview: true},
	}
	return m, st, msg, fetcher
}

var reviewFreshnessObservation = ports.PRObservation{Fetched: true, URL: "pr1", Review: domain.ReviewChangesRequest}

func TestReviewCommentNudge_ResolvedBeforeDeliveryIsNotSent(t *testing.T) {
	m, _, msg, fetcher := reviewFreshnessFixture(t, idle("mer-1"))
	// Stored row still unresolved; the provider has resolved the thread since.
	fetcher.threads = []ports.SCMReviewThreadObservation{{ID: "T1", Resolved: true}}

	if err := m.ApplyPRObservation(ctx, "mer-1", reviewFreshnessObservation); err != nil {
		t.Fatal(err)
	}
	if len(msg.msgs) != 0 {
		t.Fatalf("resolved thread was delivered as unresolved:\n%s", msg.msgs[0])
	}
	if fetcher.calls != 1 {
		t.Fatalf("fetch calls = %d, want 1 delivery-time read", fetcher.calls)
	}

	// The thread is marked handled, so unchanged stored rows do not refetch.
	if err := m.ApplyPRObservation(ctx, "mer-1", reviewFreshnessObservation); err != nil {
		t.Fatal(err)
	}
	if fetcher.calls != 1 || len(msg.msgs) != 0 {
		t.Fatalf("second poll: fetch calls = %d, messages = %d; want 1 and 0", fetcher.calls, len(msg.msgs))
	}
}

func TestReviewCommentNudge_TextShowsFetchTimestamp(t *testing.T) {
	m, _, msg, _ := reviewFreshnessFixture(t, idle("mer-1"))

	if err := m.ApplyPRObservation(ctx, "mer-1", reviewFreshnessObservation); err != nil {
		t.Fatal(err)
	}
	if len(msg.msgs) != 1 {
		t.Fatalf("want one review nudge, got %d", len(msg.msgs))
	}
	if strings.Contains(msg.msgs[0], "as of just now") {
		t.Fatalf("nudge still uses the placeholder time:\n%s", msg.msgs[0])
	}
	want := "as of " + reviewFreshnessNow.Format(time.RFC3339) + ", when AO last read"
	if !strings.Contains(msg.msgs[0], want) {
		t.Fatalf("nudge missing fetch timestamp %q:\n%s", want, msg.msgs[0])
	}
}

func TestReviewCommentNudge_ResolveWhileWorkerActiveIsCheckedAtDelivery(t *testing.T) {
	m, st, msg, fetcher := reviewFreshnessFixture(t, working("mer-1"))

	// Active worker: nothing is built, so no provider read and no paste.
	if err := m.ApplyPRObservation(ctx, "mer-1", reviewFreshnessObservation); err != nil {
		t.Fatal(err)
	}
	if len(msg.msgs) != 0 || fetcher.calls != 0 {
		t.Fatalf("active worker got messages=%d fetches=%d; want 0 and 0", len(msg.msgs), fetcher.calls)
	}

	// The worker fixes and resolves the thread while it is still active.
	fetcher.threads = []ports.SCMReviewThreadObservation{{ID: "T1", Resolved: true}}
	st.sessions["mer-1"] = idle("mer-1")
	if err := m.ApplyPRObservation(ctx, "mer-1", reviewFreshnessObservation); err != nil {
		t.Fatal(err)
	}
	if len(msg.msgs) != 0 {
		t.Fatalf("comment resolved before idle was delivered:\n%s", msg.msgs[0])
	}
	if fetcher.calls != 1 {
		t.Fatalf("fetch calls = %d, want the one delivery-time read", fetcher.calls)
	}
}

func TestReviewCommentNudge_StillUnresolvedAfterActiveIsDelivered(t *testing.T) {
	m, st, msg, fetcher := reviewFreshnessFixture(t, working("mer-1"))

	if err := m.ApplyPRObservation(ctx, "mer-1", reviewFreshnessObservation); err != nil {
		t.Fatal(err)
	}
	st.sessions["mer-1"] = idle("mer-1")
	if err := m.ApplyPRObservation(ctx, "mer-1", reviewFreshnessObservation); err != nil {
		t.Fatal(err)
	}
	if len(msg.msgs) != 1 || !strings.Contains(msg.msgs[0], "fix this") {
		t.Fatalf("want the held comment delivered once idle, got %v", msg.msgs)
	}
	if fetcher.calls != 1 {
		t.Fatalf("fetch calls = %d, want 1", fetcher.calls)
	}
}

func TestReviewCommentNudge_FetchErrorHoldsNudgeUntilRetry(t *testing.T) {
	m, _, msg, fetcher := reviewFreshnessFixture(t, idle("mer-1"))
	fetcher.err = errors.New("provider unreachable")

	if err := m.ApplyPRObservation(ctx, "mer-1", reviewFreshnessObservation); err == nil {
		t.Fatal("want fetch error surfaced to the observer for a retry")
	}
	if len(msg.msgs) != 0 {
		t.Fatalf("nudge sent without a fresh provider read: %v", msg.msgs)
	}

	fetcher.err = nil
	if err := m.ApplyPRObservation(ctx, "mer-1", reviewFreshnessObservation); err != nil {
		t.Fatal(err)
	}
	if len(msg.msgs) != 1 {
		t.Fatalf("want the nudge delivered on retry, got %d", len(msg.msgs))
	}
}

func TestReviewCommentNudge_NoFetcherHoldsNudge(t *testing.T) {
	m, st, msg := newManager()
	m.SetReviewThreadFetcher(nil)
	st.sessions["mer-1"] = idle("mer-1")
	st.comments["pr1"] = []domain.PullRequestComment{
		{ID: "1", ThreadID: "T1", Body: "fix this", AutoInjectReview: true},
	}

	err := m.ApplyPRObservation(ctx, "mer-1", reviewFreshnessObservation)
	if !errors.Is(err, errReviewFreshnessUnavailable) {
		t.Fatalf("err = %v, want errReviewFreshnessUnavailable", err)
	}
	if len(msg.msgs) != 0 {
		t.Fatalf("nudge sent with no way to confirm freshness: %v", msg.msgs)
	}
}

func TestReviewCommentsSignatureChangesOnResolve(t *testing.T) {
	unresolved := []ports.PRCommentObservation{{ID: "c1", ThreadID: "t1", Body: "fix"}}
	resolved := []ports.PRCommentObservation{{ID: "c1", ThreadID: "t1", Body: "fix", Resolved: true}}
	if got, old := reviewCommentsSignature(resolved), reviewCommentsSignature(unresolved); got == old {
		t.Fatalf("resolving the same comment kept signature %q", got)
	}
}
