package orchestratortick

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
)

const (
	// ActionMerge is one provider merge guarded by the evaluated head.
	ActionMerge = "merge"
	// ActionClose is one lifecycle termination of a completed worker.
	ActionClose = "close"

	// CompletionActionBlocked records a precondition failure. The next tick
	// re-evaluates fresh facts; it never reuses a stale head or approval.
	CompletionActionBlocked = "blocked"
	// CompletionActionSuperseded marks an action that can never succeed for
	// the facts it pinned (a head that advanced, or a PR closed unmerged).
	// It is never retried or reported again.
	CompletionActionSuperseded = "superseded"
)

var headSHAPattern = regexp.MustCompile(`^[0-9a-fA-F]{40}([0-9a-fA-F]{24})?$`)

// CompletionNote is one worker's reconciled completion state for status and
// reporting. It is derived from durable PR, branch, activity, and obligation
// facts on every tick; it is not stored as display status.
type CompletionNote struct {
	Host      string
	Session   string
	PRs       []int
	Merged    int
	Closed    bool
	Remaining []string
	Errors    []string
}

// CompletionLines renders completion notes for `ao orchestrator tick status`
// and for the tick message.
func CompletionLines(notes []CompletionNote) []string {
	lines := make([]string, 0, len(notes))
	for _, note := range notes {
		line := fmt.Sprintf("%s/%s", note.Host, note.Session)
		if len(note.PRs) > 0 {
			parts := make([]string, 0, len(note.PRs))
			for _, pr := range note.PRs {
				parts = append(parts, fmt.Sprintf("#%d", pr))
			}
			line += " prs=" + strings.Join(parts, ",")
		}
		if note.Merged > 0 {
			line += fmt.Sprintf(" merged=#%d", note.Merged)
		}
		if note.Closed {
			line += " closed"
		}
		if len(note.Remaining) > 0 {
			line += " remaining: " + strings.Join(note.Remaining, "; ")
		}
		for _, err := range note.Errors {
			line += " error: " + err
		}
		lines = append(lines, line)
	}
	return lines
}

// MergeBlockers lists every reason a pull request must not be merged now.
// A missing durable merge authorization is reported separately from provider
// readiness so the exact blocker stays visible.
func MergeBlockers(cfg Config, pr PullRequest) []string {
	var out []string
	switch {
	case strings.TrimSpace(pr.HeadSHA) == "":
		out = append(out, "head SHA is unknown")
	case !headSHAPattern.MatchString(strings.TrimSpace(pr.HeadSHA)):
		out = append(out, "head SHA is invalid")
	}
	if pr.Draft {
		out = append(out, "pull request is a draft")
	}
	if !strings.EqualFold(strings.TrimSpace(pr.Mergeable), "MERGEABLE") {
		out = append(out, fmt.Sprintf("mergeability is %s", oneOf(pr.Mergeable, "unknown")))
	}
	if summary := CheckSummary(pr.Checks); summary != "green" {
		out = append(out, "required checks are "+summary)
	}
	if blocker := reviewBlocker(pr); blocker != "" {
		out = append(out, blocker)
	}
	if n := unresolvedThreads(pr.Threads); n > 0 {
		out = append(out, fmt.Sprintf("%d unresolved review thread(s)", n))
	}
	for _, label := range pr.Labels {
		if holdLabel(cfg, label) {
			out = append(out, "hold label "+strings.TrimSpace(label))
		}
	}
	return out
}

func reviewBlocker(pr PullRequest) string {
	switch strings.ToLower(strings.TrimSpace(pr.AOReviewStatus)) {
	case "up_to_date":
		return ""
	case "changes_requested":
		return "AO review changes requested"
	}
	switch strings.ToUpper(strings.TrimSpace(pr.ReviewDecision)) {
	case "CHANGES_REQUESTED":
		return "review changes requested"
	case "REVIEW_REQUIRED":
		return "review approval required"
	}
	// A provider APPROVED rollup is not proof for the current head: GitHub
	// keeps it after a push unless branch protection dismisses stale reviews.
	// Require an approval review whose commit is the exact head, or AO's own
	// current-head native approval above.
	if !currentHeadApproved(pr) {
		return "no current-head approval"
	}
	return ""
}

func currentHeadApproved(pr PullRequest) bool {
	head := strings.ToLower(strings.TrimSpace(pr.HeadSHA))
	if head == "" {
		return false
	}
	for _, review := range pr.Reviews {
		if !strings.EqualFold(strings.TrimSpace(review.State), "APPROVED") {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(review.CommitID), head) {
			return true
		}
	}
	return false
}

func unresolvedThreads(threads []ReviewThread) int {
	n := 0
	for _, thread := range threads {
		if thread.Resolved || thread.Outdated || strings.TrimSpace(thread.Body) == "" {
			continue
		}
		n++
	}
	return n
}

func holdLabel(cfg Config, label string) bool {
	label = strings.ToLower(strings.TrimSpace(label))
	if label == "" {
		return false
	}
	for _, hold := range cfg.HoldLabels {
		if strings.EqualFold(label, strings.TrimSpace(hold)) {
			return true
		}
	}
	return false
}

func shortHead(value string) string {
	value = strings.TrimSpace(value)
	if len(value) > 7 {
		return value[:7]
	}
	return value
}

func oneOf(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

// ownedByWorker reports whether a PR belongs to a worker, either because the
// daemon already attributed its number or because its source branch is the
// worker branch or a stacked child of it.
// applyWorkerReview attaches AO's current-head native review decision to a
// fresh provider PR observation.
func applyWorkerReview(worker Worker, pr PullRequest) PullRequest {
	for _, review := range worker.Reviews {
		if review.PRNumber == pr.Number && strings.EqualFold(strings.TrimSpace(review.TargetSHA), strings.TrimSpace(pr.HeadSHA)) {
			pr.AOReviewStatus = review.Status
			return pr
		}
	}
	return pr
}

func ownedByWorker(worker Worker, number int, head string) bool {
	for _, pr := range worker.PRs {
		if pr.Number == number {
			return true
		}
	}
	if worker.PRNumber == number {
		return true
	}
	branch := strings.TrimSpace(worker.Branch)
	head = strings.TrimSpace(head)
	if branch == "" || head == "" {
		return false
	}
	return head == branch || strings.HasPrefix(head, branch+"/")
}

// completionPaused rechecks the explicit stop flag immediately before each
// mutation, so a pause written during a tick still wins over queued work.
func (r *Runner) completionPaused() (string, bool) {
	if r.Config.Paused {
		return "RW_TICK_PAUSED=1", true
	}
	if r.Store == nil {
		return "", false
	}
	reason, paused, err := r.Store.Paused()
	if err != nil {
		return "maintenance flag unreadable: " + err.Error(), true
	}
	return reason, paused
}

func (r *Runner) reconcileCompletion(ctx context.Context, facts Facts, now time.Time) ([]CompletionNote, bool, error) {
	if r.Store == nil {
		return nil, false, nil
	}
	state, _, err := r.Store.Completion()
	if err != nil {
		return nil, false, fmt.Errorf("completion state unreadable: %w", err)
	}
	if state.Sessions == nil {
		state.Sessions = map[string]SessionCompletion{}
	}
	if facts.GitHub == nil {
		return nil, false, nil
	}
	workers := map[string]Worker{}
	hostReachable := map[string]bool{}
	for _, host := range facts.Hosts {
		hostReachable[host.Host] = host.Err == nil
		for _, worker := range host.Sessions {
			if !worker.Terminated && !strings.EqualFold(worker.Kind, "orchestrator") {
				workers[completionKey(host.Host, worker.ID)] = worker
			}
		}
	}
	notes := map[string]*CompletionNote{}
	closeEligible := map[string]bool{}
	changed := false

	// Derive eligibility and queue one idempotent merge action per ready PR.
	for _, host := range facts.Hosts {
		for _, worker := range host.Sessions {
			if worker.Terminated || strings.EqualFold(worker.Kind, "orchestrator") {
				continue
			}
			key := completionKey(host.Host, worker.ID)
			session := state.Sessions[key]
			session.Host, session.Session = host.Host, worker.ID
			if session.Actions == nil {
				session.Actions = map[string]CompletionAction{}
			}
			note, wantsClose := r.reconcileWorker(session, worker, facts)
			note.Host, note.Session = host.Host, worker.ID
			if len(note.PRs) == 0 && len(session.Obligations) == 0 && len(session.Actions) == 0 && !wantsClose {
				continue
			}
			addNote(notes, key, note)

			for _, pr := range ownedOpenPRs(worker, facts.GitHub) {
				pr = applyWorkerReview(worker, pr)
				if blockers := MergeBlockers(r.Config, pr); len(blockers) > 0 {
					noteFor(notes, key).Remaining = appendUniqueStrings(noteFor(notes, key).Remaining,
						fmt.Sprintf("PR #%d: %s", pr.Number, strings.Join(blockers, ", ")))
					continue
				}
				if !r.Config.MergeAuthorized {
					noteFor(notes, key).Remaining = appendUniqueStrings(noteFor(notes, key).Remaining,
						fmt.Sprintf("PR #%d is ready; merge authorization missing (set RW_TICK_MERGE_AUTHORIZED=1)", pr.Number))
					continue
				}
				if r.Merger == nil {
					noteFor(notes, key).Errors = appendUniqueStrings(noteFor(notes, key).Errors,
						fmt.Sprintf("PR #%d is ready but the merger is not configured", pr.Number))
					continue
				}
				actionKey := fmt.Sprintf("merge:%d@%s", pr.Number, strings.ToLower(pr.HeadSHA))
				if existing, exists := session.Actions[actionKey]; !exists || existing.Status == CompletionActionSuperseded {
					session.Actions[actionKey] = CompletionAction{
						Kind: ActionMerge, Status: CompletionActionPending, PR: pr.Number,
						HeadSHA: strings.ToLower(pr.HeadSHA), AOReviewStatus: pr.AOReviewStatus, UpdatedAt: now,
					}
					changed = true
				}
			}

			remaining := append([]string(nil), noteFor(notes, key).Remaining...)
			if !stringSlicesEqual(session.Remaining, remaining) {
				session.Remaining = remaining
				changed = true
			}
			session.UpdatedAt = now
			if wantsClose {
				closeEligible[key] = true
				if existing, exists := session.Actions["close"]; !exists || existing.Status == CompletionActionSuperseded {
					session.Actions["close"] = CompletionAction{
						Kind: ActionClose, Status: CompletionActionPending, UpdatedAt: now,
					}
					changed = true
				}
			}
			state.Sessions[key] = session
		}
	}

	// Retry pending merges before closure so a merge that just landed is seen
	// by that worker's next tick. A completed action is never replayed.
	for _, key := range sortedKeys(state.Sessions) {
		session := state.Sessions[key]
		if session.Actions == nil {
			session.Actions = map[string]CompletionAction{}
		}
		for _, actionKey := range sortedActionKeys(session.Actions) {
			action := session.Actions[actionKey]
			if action.Status == CompletionActionCompleted || action.Status == CompletionActionSuperseded || action.Kind != ActionMerge {
				continue
			}
			worker, workerPresent := workers[key]
			if !hostReachable[session.Host] {
				// A transient host failure is not proof the worker is gone.
				continue
			}
			if !workerPresent {
				action.Status = CompletionActionSuperseded
				action.LastError = "session is no longer present on its host"
				action.UpdatedAt = now
				session.Actions[actionKey] = action
				state.Sessions[key] = session
				changed = true
				continue
			}
			updated, note, didChange := r.retryAction(ctx, worker, session, action, facts.GitHub, now)
			if didChange {
				session.Actions[actionKey] = updated
				state.Sessions[key] = session
				changed = true
			}
			if note != nil {
				addNote(notes, key, *note)
			}
		}
	}

	// Complete close actions only after the lifecycle operation proves the
	// session terminated. Retrying a pending close is safe: Kill is an
	// idempotent lifecycle operation that preserves the workspace.
	for _, key := range sortedKeys(state.Sessions) {
		session := state.Sessions[key]
		action, ok := session.Actions["close"]
		if !ok || action.Status == CompletionActionCompleted || action.Status == CompletionActionSuperseded {
			continue
		}
		if !hostReachable[session.Host] {
			// A transient host failure is not proof the close is no longer due.
			continue
		}
		if !r.Config.CloseCompleted || !closeEligible[key] {
			// A queued close is only a request. The current tick must still
			// prove the worker qualifies: an added obligation, renewed activity,
			// another open PR, or turning the opt-in off cancels it.
			action.Status = CompletionActionSuperseded
			action.LastError = "close no longer eligible on this tick"
			action.UpdatedAt = now
			session.Actions["close"] = action
			session.UpdatedAt = now
			state.Sessions[key] = session
			changed = true
			continue
		}
		if reason, paused := r.completionPaused(); paused {
			action.Status = CompletionActionPending
			action.LastError = "paused before close: " + reason
			action.UpdatedAt = now
			session.Actions["close"] = action
			state.Sessions[key] = session
			note := noteFor(notes, key)
			note.Host, note.Session = session.Host, session.Session
			note.Errors = appendUniqueStrings(note.Errors, action.LastError)
			changed = true
			continue
		}
		if err := r.Daemon.Kill(ctx, session.Host, session.Session); err != nil {
			action.Status = CompletionActionPending
			action.Attempts++
			action.LastError = err.Error()
			action.UpdatedAt = now
			session.Actions["close"] = action
			state.Sessions[key] = session
			note := noteFor(notes, key)
			note.Host, note.Session = session.Host, session.Session
			note.Errors = appendUniqueStrings(note.Errors, "close failed: "+err.Error())
			changed = true
			continue
		}
		action.Status = CompletionActionCompleted
		action.LastError = ""
		action.UpdatedAt = now
		session.Actions["close"] = action
		session.Remaining = nil
		session.UpdatedAt = now
		state.Sessions[key] = session
		note := noteFor(notes, key)
		note.Host, note.Session = session.Host, session.Session
		note.Closed = true
		changed = true
	}

	if changed {
		if err := r.Store.SaveCompletion(state); err != nil {
			return notesList(notes), changed, fmt.Errorf("save completion state: %w", err)
		}
	}
	return notesList(notes), changed, nil
}

func (r *Runner) reconcileWorker(session SessionCompletion, worker Worker, facts Facts) (CompletionNote, bool) {
	note := CompletionNote{}
	gh := facts.GitHub
	open := ownedOpenPRs(worker, gh)
	merged := ownedMergedPRs(worker, gh)
	closed := ownedClosedPRs(worker, gh)
	for _, pr := range open {
		note.PRs = append(note.PRs, pr.Number)
	}
	for _, pr := range merged {
		note.PRs = append(note.PRs, pr.Number)
		note.Merged = pr.Number
	}
	for _, pr := range closed {
		note.PRs = append(note.PRs, pr.Number)
	}
	if len(merged) == 0 {
		// A worker with no merged or closed PR is not a completion candidate,
		// even if it only owns open work.
		return note, false
	}
	if len(open) > 0 {
		for _, pr := range open {
			note.Remaining = append(note.Remaining, fmt.Sprintf("owned PR #%d is still open", pr.Number))
		}
		return note, false
	}
	if strings.EqualFold(strings.TrimSpace(worker.Activity), "active") {
		note.Remaining = append(note.Remaining, "agent is still active")
		return note, false
	}
	if worker.BranchState == nil || !worker.BranchState.Known {
		note.Remaining = append(note.Remaining, "branch push state is unknown; cannot prove no unpublished work")
		return note, false
	}
	if worker.BranchState.Unpushed > 0 {
		note.Remaining = append(note.Remaining, fmt.Sprintf("%d unpublished commit(s) on %s", worker.BranchState.Unpushed, oneOf(worker.Branch, "the branch")))
		return note, false
	}
	for _, obligation := range session.Obligations {
		reason := strings.TrimSpace(obligation.Reason)
		if condition := strings.TrimSpace(obligation.CompletionCondition); condition != "" {
			reason += " (until " + condition + ")"
		}
		if reason != "" {
			note.Remaining = append(note.Remaining, reason)
		}
	}
	if len(note.Remaining) > 0 {
		return note, false
	}
	if !r.Config.CloseCompleted {
		note.Remaining = append(note.Remaining, "completion closure not authorized (set RW_TICK_CLOSE_COMPLETED=1)")
		return note, false
	}
	return note, true
}

func (r *Runner) retryAction(ctx context.Context, worker Worker, session SessionCompletion, action CompletionAction, gh *GitHubState, now time.Time) (CompletionAction, *CompletionNote, bool) {
	note := &CompletionNote{Host: session.Host, Session: session.Session}
	switch action.Kind {
	case ActionMerge:
		pr, open := openPR(gh, action.PR)
		if !open {
			if mergedPR(gh, action.PR) {
				action.Status = CompletionActionCompleted
				action.LastError = ""
				action.UpdatedAt = now
				return action, note, true
			}
			action.Status = CompletionActionSuperseded
			action.LastError = fmt.Sprintf("PR #%d closed without merging", action.PR)
			action.UpdatedAt = now
			note.Errors = append(note.Errors, action.LastError)
			return action, note, true
		}
		if !strings.EqualFold(pr.HeadSHA, action.HeadSHA) {
			action.Status = CompletionActionSuperseded
			action.LastError = fmt.Sprintf("PR #%d head changed from %s to %s", action.PR, shortHead(action.HeadSHA), shortHead(pr.HeadSHA))
			action.UpdatedAt = now
			note.Errors = append(note.Errors, action.LastError)
			return action, note, true
		}
		pr = applyWorkerReview(worker, pr)
		if blockers := MergeBlockers(r.Config, pr); len(blockers) > 0 {
			action.Status = CompletionActionBlocked
			action.LastError = "PR #" + fmt.Sprint(action.PR) + " no longer mergeable: " + strings.Join(blockers, ", ")
			action.UpdatedAt = now
			note.Errors = append(note.Errors, action.LastError)
			return action, note, true
		}
		if !r.Config.MergeAuthorized || r.Merger == nil {
			action.Status = CompletionActionPending
			action.LastError = "merge authorization or merger is unavailable"
			action.UpdatedAt = now
			note.Errors = append(note.Errors, action.LastError)
			return action, note, true
		}
		if reason, paused := r.completionPaused(); paused {
			action.Status = CompletionActionPending
			action.LastError = "paused before merge: " + reason
			action.UpdatedAt = now
			note.Errors = append(note.Errors, action.LastError)
			return action, note, true
		}
		action.Attempts++
		result, err := r.Merger.Merge(ctx, MergeRequest{Repo: r.Config.Repo, Number: action.PR, ExpectedHeadSHA: action.HeadSHA})
		if err != nil {
			switch {
			case errors.Is(err, ErrMergeHeadChanged):
				action.Status = CompletionActionSuperseded
			case errors.Is(err, ErrMergeNotReady):
				action.Status = CompletionActionBlocked
			default:
				action.Status = CompletionActionPending
			}
			action.LastError = err.Error()
			action.UpdatedAt = now
			note.Errors = append(note.Errors, "merge failed: "+err.Error())
			return action, note, true
		}
		action.Status = CompletionActionCompleted
		action.MergeCommitSHA = result.MergeCommitSHA
		action.LastError = ""
		action.UpdatedAt = now
		return action, note, true
	case ActionClose:
		// A pending close whose worker is already absent is verified by the
		// idempotent lifecycle call in reconcileCompletion, not here.
		return action, nil, false
	default:
		return action, nil, false
	}
}

// ErrMergeHeadChanged reports that the provider head advanced after the
// reconciler evaluated it.
var ErrMergeHeadChanged = errors.New("pull request head changed")

// ErrMergeNotReady reports that the provider refused a merge that no longer
// meets its protections.
var ErrMergeNotReady = errors.New("pull request is not mergeable")

func completionKey(host, session string) string {
	host = strings.TrimSpace(host)
	if host == "" {
		host = "local"
	}
	return host + "/" + strings.TrimSpace(session)
}

func ownedOpenPRs(worker Worker, gh *GitHubState) []PullRequest {
	var out []PullRequest
	for _, pr := range gh.OpenPRs {
		if ownedByWorker(worker, pr.Number, pr.Head) {
			out = append(out, pr)
		}
	}
	return out
}

func ownedMergedPRs(worker Worker, gh *GitHubState) []MergedPR {
	var out []MergedPR
	for _, pr := range gh.MergedPRs {
		if ownedByWorker(worker, pr.Number, pr.Head) {
			out = append(out, pr)
		}
	}
	return out
}

func ownedClosedPRs(worker Worker, gh *GitHubState) []ClosedPR {
	var out []ClosedPR
	for _, pr := range gh.ClosedPRs {
		if ownedByWorker(worker, pr.Number, pr.Head) {
			out = append(out, pr)
		}
	}
	return out
}

func openPR(gh *GitHubState, number int) (PullRequest, bool) {
	for _, pr := range gh.OpenPRs {
		if pr.Number == number {
			return pr, true
		}
	}
	return PullRequest{}, false
}

func mergedPR(gh *GitHubState, number int) bool {
	for _, pr := range gh.MergedPRs {
		if pr.Number == number {
			return true
		}
	}
	return false
}

func addNote(notes map[string]*CompletionNote, key string, note CompletionNote) {
	existing := noteFor(notes, key)
	if existing.Host == "" {
		existing.Host = note.Host
	}
	if existing.Session == "" {
		existing.Session = note.Session
	}
	existing.PRs = appendUniqueInts(existing.PRs, note.PRs...)
	if note.Merged != 0 {
		existing.Merged = note.Merged
	}
	existing.Closed = existing.Closed || note.Closed
	existing.Remaining = appendUniqueStrings(existing.Remaining, note.Remaining...)
	existing.Errors = appendUniqueStrings(existing.Errors, note.Errors...)
}

func noteFor(notes map[string]*CompletionNote, key string) *CompletionNote {
	note, ok := notes[key]
	if !ok {
		note = &CompletionNote{}
		notes[key] = note
	}
	return note
}

func notesList(notes map[string]*CompletionNote) []CompletionNote {
	keys := make([]string, 0, len(notes))
	for key := range notes {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]CompletionNote, 0, len(keys))
	for _, key := range keys {
		out = append(out, *notes[key])
	}
	return out
}

func sortedKeys(values map[string]SessionCompletion) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func sortedActionKeys(values map[string]CompletionAction) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func appendUniqueInts(dst []int, values ...int) []int {
	seen := make(map[int]bool, len(dst))
	for _, value := range dst {
		seen[value] = true
	}
	for _, value := range values {
		if !seen[value] {
			dst = append(dst, value)
			seen[value] = true
		}
	}
	return dst
}

func appendUniqueStrings(dst []string, values ...string) []string {
	seen := make(map[string]bool, len(dst))
	for _, value := range dst {
		seen[value] = true
	}
	for _, value := range values {
		if value == "" || seen[value] {
			continue
		}
		dst = append(dst, value)
		seen[value] = true
	}
	return dst
}

func stringSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
