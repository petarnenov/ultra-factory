package scm

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

// recordingAnnouncer keeps every merge announcement the observer makes.
type recordingAnnouncer struct {
	calls []string
}

func (r *recordingAnnouncer) AnnounceMerged(_ context.Context, worker domain.SessionID, _ string, number int, sha string) error {
	r.calls = append(r.calls, fmt.Sprintf("%s #%d %s", worker, number, sha))
	return nil
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
	obs.mergeAnnouncer = announcer
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
