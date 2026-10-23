package scm

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

// recordingAnnouncer keeps every PR outcome announcement the observer makes.
type recordingAnnouncer struct {
	calls  []string
	closed []string
	tries  int
	seen   map[string]bool
	// failFirst makes the first announcement fail; later ones succeed.
	failFirst bool
}

func (r *recordingAnnouncer) AnnounceMerged(_ context.Context, worker domain.SessionID, prURL string, number int, sha string) error {
	r.tries++
	if r.failFirst && r.tries == 1 {
		return errors.New("announce failed")
	}
	key := sha
	if key == "" {
		key = fmt.Sprintf("%s#%d", prURL, number)
	}
	if r.seen == nil {
		r.seen = make(map[string]bool)
	}
	if r.seen[key] {
		return nil
	}
	r.seen[key] = true
	r.calls = append(r.calls, fmt.Sprintf("%s #%d %s", worker, number, sha))
	return nil
}

func (r *recordingAnnouncer) AnnounceClosed(_ context.Context, worker domain.SessionID, _ string, number int, _ time.Time) error {
	r.tries++
	if r.failFirst && r.tries == 1 {
		return errors.New("announce failed")
	}
	r.closed = append(r.closed, fmt.Sprintf("%s #%d", worker, number))
	return nil
}

func TestPoll_AnnouncesAnUnmergedCloseOnceAndNeverAsAMerge(t *testing.T) {
	store := testStoreWithSession()
	local := knownPR(1)
	local.Merged = false
	local.Closed = false
	store.prs["p-1"] = []domain.PullRequest{local}

	closedObs := testObs(1)
	closedObs.PR.Closed = true
	closedObs.PR.State = "closed"

	repoKey := prKey(testRepo, 0)
	commitCacheKey := commitKey(testRepo, "sha1")
	provider := &fakeProvider{
		repoGuards:  map[string]ports.SCMGuardResult{repoKey: {ETag: "v1", NotModified: true}},
		checkGuards: map[string]ports.SCMGuardResult{commitCacheKey: {ETag: "ci1", NotModified: true}},
		observations: map[string]ports.SCMObservation{
			prKey(testRepo, 1): closedObs,
		},
	}
	announcer := &recordingAnnouncer{}
	obs := newTestObserver(store, provider, &fakeLifecycle{}, time.Unix(1000, 0).UTC())
	obs.prOutcomes = announcer
	for range 2 {
		obs.Cache.RepoPRListETag[repoKey] = "v1"
		obs.Cache.CommitChecksETag[commitCacheKey] = "ci1"
		if err := obs.Poll(context.Background()); err != nil {
			t.Fatal(err)
		}
	}

	if len(announcer.closed) != 1 || len(announcer.calls) != 0 {
		t.Fatalf("closed/merged announcements = %v/%v, want one close only", announcer.closed, announcer.calls)
	}
}

func TestPoll_AnnouncesAMergeOnceOnTheMergedTransition(t *testing.T) {
	store := testStoreWithSession()
	local := knownPR(1)
	local.Merged = false
	local.Closed = false
	store.prs["p-1"] = []domain.PullRequest{local}

	mergedObs := testObs(1)
	mergedObs.PR.Merged = true
	mergedObs.PR.State = "merged"
	mergedObs.PR.MergeCommitSHA = "merge-sha"

	repoKey := prKey(testRepo, 0)
	commitCacheKey := commitKey(testRepo, "sha1")
	provider := &fakeProvider{
		repoGuards:  map[string]ports.SCMGuardResult{repoKey: {ETag: "v1", NotModified: true}},
		checkGuards: map[string]ports.SCMGuardResult{commitCacheKey: {ETag: "ci1", NotModified: true}},
		observations: map[string]ports.SCMObservation{
			prKey(testRepo, 1): mergedObs,
		},
	}
	announcer := &recordingAnnouncer{}
	now := time.Unix(1000, 0).UTC()
	obs := newTestObserver(store, provider, &fakeLifecycle{}, now)
	obs.prOutcomes = announcer
	obs.Cache.RepoPRListETag[repoKey] = "v1"
	obs.Cache.CommitChecksETag[commitCacheKey] = "ci1"

	if err := obs.Poll(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(announcer.calls) != 1 {
		t.Fatalf("announcements = %v, want one on the merged transition", announcer.calls)
	}

	// The merge is now persisted; a later poll must not announce it again.
	obs.Cache.RepoPRListETag[repoKey] = "v1"
	obs.Cache.CommitChecksETag[commitCacheKey] = "ci1"
	if err := obs.Poll(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(announcer.calls) != 1 {
		t.Fatalf("repeated announcement on a later poll: %v", announcer.calls)
	}
}

func TestPoll_RetriesTheMergeAnnouncementWhenItFails(t *testing.T) {
	store := testStoreWithSession()
	local := knownPR(1)
	local.Merged = false
	local.Closed = false
	store.prs["p-1"] = []domain.PullRequest{local}

	mergedObs := testObs(1)
	mergedObs.PR.Merged = true
	mergedObs.PR.State = "merged"
	mergedObs.PR.MergeCommitSHA = "merge-sha"

	repoKey := prKey(testRepo, 0)
	commitCacheKey := commitKey(testRepo, "sha1")
	provider := &fakeProvider{
		repoGuards:  map[string]ports.SCMGuardResult{repoKey: {ETag: "v1", NotModified: true}},
		checkGuards: map[string]ports.SCMGuardResult{commitCacheKey: {ETag: "ci1", NotModified: true}},
		observations: map[string]ports.SCMObservation{
			prKey(testRepo, 1): mergedObs,
		},
	}
	announcer := &recordingAnnouncer{failFirst: true}
	now := time.Unix(1000, 0).UTC()
	obs := newTestObserver(store, provider, &fakeLifecycle{}, now)
	obs.prOutcomes = announcer
	obs.Cache.RepoPRListETag[repoKey] = "v1"
	obs.Cache.CommitChecksETag[commitCacheKey] = "ci1"
	currentTime := now
	obs.clock = func() time.Time { return currentTime }

	if err := obs.Poll(context.Background()); err != nil {
		t.Fatal(err)
	}
	if announcer.tries != 1 || len(announcer.calls) != 0 {
		t.Fatalf("announcement attempts/sends = %d/%d, want 1/0", announcer.tries, len(announcer.calls))
	}
	// The failed announce must not make the merge durable, or the transition
	// would never be seen again.
	if len(store.writes) != 0 {
		t.Fatalf("write occurred after a failed announcement: %+v", store.writes)
	}

	currentTime = currentTime.Add(DefaultPRMaxAge + time.Second)
	provider.repoGuards[repoKey] = ports.SCMGuardResult{ETag: "v2"}
	provider.checkGuards[commitCacheKey] = ports.SCMGuardResult{ETag: "ci2"}
	if err := obs.Poll(context.Background()); err != nil {
		t.Fatal(err)
	}
	if announcer.tries != 2 || len(announcer.calls) != 1 {
		t.Fatalf("announcement attempts/sends = %d/%d, want 2/1", announcer.tries, len(announcer.calls))
	}
	if len(store.writes) == 0 || !store.writes[len(store.writes)-1].pr.Merged {
		t.Fatalf("merged not persisted after a successful announcement: %+v", store.writes)
	}
}

func TestPoll_DedupesAnnouncementAfterDatabaseWriteFailure(t *testing.T) {
	store := testStoreWithSession()
	store.prs["p-1"] = []domain.PullRequest{knownPR(1)}
	store.writeErr = errors.New("database write failed")

	mergedObs := testObs(1)
	mergedObs.PR.Merged = true
	mergedObs.PR.State = "merged"
	mergedObs.PR.MergeCommitSHA = "merge-sha"
	repoKey := prKey(testRepo, 0)
	commitCacheKey := commitKey(testRepo, "sha1")
	provider := &fakeProvider{
		repoGuards:  map[string]ports.SCMGuardResult{repoKey: {ETag: "v1", NotModified: true}},
		checkGuards: map[string]ports.SCMGuardResult{commitCacheKey: {ETag: "ci1", NotModified: true}},
		observations: map[string]ports.SCMObservation{
			prKey(testRepo, 1): mergedObs,
		},
	}
	announcer := &recordingAnnouncer{}
	currentTime := time.Unix(1000, 0).UTC()
	obs := newTestObserver(store, provider, &fakeLifecycle{}, currentTime)
	obs.clock = func() time.Time { return currentTime }
	obs.prOutcomes = announcer
	obs.Cache.RepoPRListETag[repoKey] = "v1"
	obs.Cache.CommitChecksETag[commitCacheKey] = "ci1"

	if err := obs.Poll(context.Background()); err != nil {
		t.Fatal(err)
	}
	if announcer.tries != 1 || len(announcer.calls) != 1 || len(store.writes) != 0 {
		t.Fatalf("after write failure: attempts=%d sends=%d writes=%d", announcer.tries, len(announcer.calls), len(store.writes))
	}

	store.writeErr = nil
	currentTime = currentTime.Add(DefaultPRMaxAge + time.Second)
	provider.repoGuards[repoKey] = ports.SCMGuardResult{ETag: "v2"}
	provider.checkGuards[commitCacheKey] = ports.SCMGuardResult{ETag: "ci2"}
	if err := obs.Poll(context.Background()); err != nil {
		t.Fatal(err)
	}
	if announcer.tries != 2 || len(announcer.calls) != 1 {
		t.Fatalf("duplicate announcement after write retry: attempts=%d sends=%d", announcer.tries, len(announcer.calls))
	}
	if len(store.writes) < 1 || !store.writes[len(store.writes)-1].pr.Merged {
		t.Fatalf("merged observation was not persisted on retry: %+v", store.writes)
	}
}

func TestPoll_RetriesTheCloseAnnouncementWhenItFails(t *testing.T) {
	store := testStoreWithSession()
	local := knownPR(1)
	local.Merged = false
	local.Closed = false
	store.prs["p-1"] = []domain.PullRequest{local}

	closedObs := testObs(1)
	closedObs.PR.Closed = true
	closedObs.PR.State = "closed"

	repoKey := prKey(testRepo, 0)
	commitCacheKey := commitKey(testRepo, "sha1")
	provider := &fakeProvider{
		repoGuards:  map[string]ports.SCMGuardResult{repoKey: {ETag: "v1", NotModified: true}},
		checkGuards: map[string]ports.SCMGuardResult{commitCacheKey: {ETag: "ci1", NotModified: true}},
		observations: map[string]ports.SCMObservation{
			prKey(testRepo, 1): closedObs,
		},
	}
	announcer := &recordingAnnouncer{failFirst: true}
	now := time.Unix(1000, 0).UTC()
	obs := newTestObserver(store, provider, &fakeLifecycle{}, now)
	obs.prOutcomes = announcer
	obs.Cache.RepoPRListETag[repoKey] = "v1"
	obs.Cache.CommitChecksETag[commitCacheKey] = "ci1"
	currentTime := now
	obs.clock = func() time.Time { return currentTime }

	if err := obs.Poll(context.Background()); err != nil {
		t.Fatal(err)
	}
	// A failed announce must not persist the close, or it is never seen again.
	if len(announcer.closed) != 0 || len(store.writes) != 0 {
		t.Fatalf("after a failed announce: closed=%v writes=%d", announcer.closed, len(store.writes))
	}

	currentTime = currentTime.Add(DefaultPRMaxAge + time.Second)
	provider.repoGuards[repoKey] = ports.SCMGuardResult{ETag: "v2"}
	provider.checkGuards[commitCacheKey] = ports.SCMGuardResult{ETag: "ci2"}
	if err := obs.Poll(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(announcer.closed) != 1 || len(store.writes) == 0 || !store.writes[len(store.writes)-1].pr.Closed {
		t.Fatalf("after retry: closed=%v writes=%+v", announcer.closed, store.writes)
	}
}
