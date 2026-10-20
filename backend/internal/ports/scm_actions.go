package ports

import (
	"context"
	"errors"
)

// ErrSCMHeadChanged indicates that the pull request advanced beyond the head
// SHA supplied with the mutation.
var ErrSCMHeadChanged = errors.New("scm: pull request head changed")

// ErrSCMNotMergeable indicates that the provider rejected the pull request's
// current mergeability state.
var ErrSCMNotMergeable = errors.New("scm: pull request not mergeable")

// ErrSCMUnsupported indicates that the configured SCM provider does not support
// the requested mutation.
var ErrSCMUnsupported = errors.New("scm: unsupported operation")

// SCMMergeMethod identifies the provider merge strategy.
type SCMMergeMethod string

// SCMMergeSquash requests a squash merge from the provider.
const SCMMergeSquash SCMMergeMethod = "squash"

// SCMMergeRequest uses ExpectedHeadSHA as a compare-and-swap guard. Provider
// implementations must reject the mutation if the live head has advanced.
type SCMMergeRequest struct {
	PR              SCMPRRef
	ExpectedHeadSHA string
	Method          SCMMergeMethod
}

// SCMMergeResult reports the provider's resulting merge commit.
type SCMMergeResult struct {
	MergeCommitSHA string
}

// SCMMerger mutates pull requests through an SCM provider.
type SCMMerger interface {
	MergePullRequest(ctx context.Context, request SCMMergeRequest) (SCMMergeResult, error)
}

// SCMReviewRequest asks the provider to request another review from Reviewer.
type SCMReviewRequest struct {
	PR       SCMPRRef
	Reviewer string
}

// SCMReviewRequester mutates pull-request review requests through an SCM provider.
type SCMReviewRequester interface {
	RequestReview(ctx context.Context, request SCMReviewRequest) error
}

// SCMReviewResolveRequest asks the provider to resolve one review thread.
type SCMReviewResolveRequest struct {
	PR       SCMPRRef
	ThreadID string
}

// SCMReviewResolver resolves pull-request review threads through an SCM provider.
type SCMReviewResolver interface {
	ResolveReviewThread(ctx context.Context, request SCMReviewResolveRequest) error
}

// SCMDraftRequest asks the provider to set a pull request's draft flag.
// Draft=false marks it ready for review; Draft=true converts it to a draft.
type SCMDraftRequest struct {
	PR    SCMPRRef
	Draft bool
}

// SCMDraftSetter flips a pull request between draft and ready for review.
// Setting the state a PR already has succeeds without a change.
type SCMDraftSetter interface {
	SetPullRequestDraft(ctx context.Context, request SCMDraftRequest) error
}
