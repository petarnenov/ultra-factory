package orchestratortick

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// closingRef matches the GitHub keywords that close an issue from a pull
// request body: "Closes #12", "fixed #3", "resolves #40".
var closingRef = regexp.MustCompile(`(?i)\b(?:close[sd]?|fix(?:e[sd])?|resolve[sd]?)\s+#(\d+)`)

// Violation is one invariant that does not hold: Text is the sentence shown to
// the orchestrator, and Key identifies it without the parts that drift.
type Violation struct {
	Key  string
	Text string
}

// Violations lists every invariant of the reconcile loop that does not hold.
// The rules are checked in code because "after X, remember Y" gets skipped.
func Violations(cfg Config, facts Facts, now time.Time) []Violation {
	var out []Violation
	if facts.GitHub == nil {
		reason := "no data"
		if facts.GitHubErr != nil {
			reason = facts.GitHubErr.Error()
		}
		out = append(out, Violation{Key: "github-unavailable", Text: fmt.Sprintf("the GitHub state is unavailable (%s); PR and CI state is unknown → check GH_TOKEN and repository access (see deploy/railway/README.md)", reason)})
	} else {
		gh := facts.GitHub
		out = append(out, behindMain(cfg, gh)...)
		out = append(out, greenPRsLeftOpen(cfg, facts, now)...)
		out = append(out, mergedPRsWithOpenIssues(gh)...)
	}
	out = append(out, idleWorkersWithoutPR(cfg, facts.Hosts, now)...)
	out = append(out, agentMix(cfg, facts.Hosts)...)
	return out
}

// 1. The integration branch contains main.
func behindMain(cfg Config, gh *GitHubState) []Violation {
	if gh.BehindMain <= 0 {
		return nil
	}
	text := fmt.Sprintf("%s is %d commit(s) behind %s (%s) → git merge origin/%s into %s and push",
		cfg.Base, gh.BehindMain, cfg.MainBranch, strings.Join(gh.BehindSHAs, ","), cfg.MainBranch, cfg.Base)
	return []Violation{{Key: text, Text: text}}
}

// 2. A green, mergeable pull request does not sit unmerged. Its review state
// decides the remaining work; a hold label keeps it off the merge path.
func greenPRsLeftOpen(cfg Config, facts Facts, now time.Time) []Violation {
	var out []Violation
	if facts.GitHub == nil {
		return nil
	}
	for _, pr := range facts.GitHub.OpenPRs {
		if pr.Draft || pr.Mergeable != "MERGEABLE" || PRCheckSummary(pr, now) != "green" {
			continue
		}
		var last time.Time
		for _, c := range pr.Checks {
			if c.CompletedAt.After(last) {
				last = c.CompletedAt
			}
		}
		if last.IsZero() {
			continue
		}
		age := now.Sub(last)
		if age < cfg.GreenPRMax {
			continue
		}
		if holdLabeled(cfg, pr) {
			continue
		}
		pr.AOReviewStatus = aoReviewStatus(facts, pr)
		if reviewWaitsOnAuthor(pr) {
			continue
		}
		out = append(out, Violation{
			Key:  fmt.Sprintf("green-pr:#%d", pr.Number),
			Text: greenPRText(pr, age),
		})
	}
	return out
}

// greenPRText names the work left: merge a clean pull request, or run the
// review that is still pending for this head.
func greenPRText(pr PullRequest, age time.Duration) string {
	text := fmt.Sprintf("#%d (%s) has been green and mergeable for %d min → review it per \"Review and merge\"",
		pr.Number, pr.Head, minutes(age))
	if blocker := reviewBlocker(pr); blocker != "" {
		return text + "; " + blocker
	}
	return text + " and merge it"
}

// reviewWaitsOnAuthor reports a verdict or thread only the worker can clear.
// Such a pull request is not a merge violation; completion reports it.
func reviewWaitsOnAuthor(pr PullRequest) bool {
	if unresolvedThreads(pr.Threads) > 0 {
		return true
	}
	if strings.EqualFold(strings.TrimSpace(pr.ReviewDecision), "CHANGES_REQUESTED") {
		return true
	}
	return strings.EqualFold(strings.TrimSpace(pr.AOReviewStatus), "changes_requested")
}

// holdLabeled reports a pull request the configuration keeps out of the merge
// path, including an operator's pull request awaiting their word.
func holdLabeled(cfg Config, pr PullRequest) bool {
	for _, label := range pr.Labels {
		if holdLabel(cfg, label) {
			return true
		}
	}
	return false
}

// aoReviewStatus finds AO's verdict for the exact head, the same fact the
// completion pass attaches through applyWorkerReview.
func aoReviewStatus(facts Facts, pr PullRequest) string {
	if status := strings.TrimSpace(pr.AOReviewStatus); status != "" {
		return status
	}
	for _, host := range facts.Hosts {
		for _, worker := range host.Sessions {
			for _, review := range worker.Reviews {
				if review.PRNumber != pr.Number {
					continue
				}
				if strings.EqualFold(strings.TrimSpace(review.TargetSHA), strings.TrimSpace(pr.HeadSHA)) {
					return review.Status
				}
			}
		}
	}
	return ""
}

// 3. A merged pull request leaves none of the issues it closes open.
func mergedPRsWithOpenIssues(gh *GitHubState) []Violation {
	var out []Violation
	for _, pr := range gh.MergedPRs {
		seen := map[int]bool{}
		var open []int
		for _, m := range closingRef.FindAllStringSubmatch(pr.Body, -1) {
			n, err := strconv.Atoi(m[1])
			if err != nil || seen[n] {
				continue
			}
			seen[n] = true
			if _, ok := gh.OpenIssues[n]; ok {
				open = append(open, n)
			}
		}
		if len(open) == 0 {
			continue
		}
		sort.Ints(open)
		refs := make([]string, len(open))
		for i, n := range open {
			refs[i] = "#" + strconv.Itoa(n)
		}
		text := fmt.Sprintf("#%d is merged but %s is still open → gh issue close per step 5 of \"Review and merge\"",
			pr.Number, strings.Join(refs, ", "))
		out = append(out, Violation{Key: text, Text: text})
	}
	return out
}

// 4. A worker does not sit idle without a pull request.
func idleWorkersWithoutPR(cfg Config, hosts []HostSessions, now time.Time) []Violation {
	var out []Violation
	for _, h := range hosts {
		for _, w := range h.Sessions {
			if !strings.HasPrefix(w.Branch, cfg.BranchPrefix) || w.Terminated || w.Activity != "idle" || w.PRNumber != 0 || w.LastActivityAt.IsZero() {
				continue
			}
			if age := now.Sub(w.LastActivityAt); age >= cfg.IdleWorkerMax {
				out = append(out, Violation{
					Key: fmt.Sprintf("idle-worker:%s/%s", h.Host, w.ID),
					Text: fmt.Sprintf("%s (%s) on %s has been idle for %d min without a PR → ao send --host %s asking what blocks it",
						w.ID, w.Branch, h.Host, minutes(age), h.Host),
				})
			}
		}
	}
	return out
}

// 5. Workers started after the agent-mix rule use both agents: never two
// live workers with the same agent on one host, and never only claude-code.
func agentMix(cfg Config, hosts []HostSessions) []Violation {
	if cfg.AgentMixSince.IsZero() {
		return nil
	}
	var out []Violation
	total, codex := 0, false
	for _, h := range hosts {
		live := map[string][]string{}
		for _, w := range h.Sessions {
			if !strings.HasPrefix(w.Branch, cfg.BranchPrefix) || w.CreatedAt.IsZero() || w.CreatedAt.Before(cfg.AgentMixSince) {
				continue
			}
			total++
			if w.Harness == "codex" {
				codex = true
			}
			if !w.Terminated {
				live[w.Harness] = append(live[w.Harness], w.ID)
			}
		}
		harnesses := make([]string, 0, len(live))
		for harness := range live {
			harnesses = append(harnesses, harness)
		}
		sort.Strings(harnesses)
		for _, harness := range harnesses {
			if ids := live[harness]; len(ids) > 1 {
				text := fmt.Sprintf("%s runs %d live %s workers (%s) → the next task on %s must use the other agent (--harness)",
					h.Host, len(ids), harness, strings.Join(ids, ", "), h.Host)
				out = append(out, Violation{Key: text, Text: text})
			}
		}
	}
	if total >= 2 && !codex {
		text := fmt.Sprintf("none of the %d workers started since %s uses codex → start the next ready task with --harness codex",
			total, cfg.AgentMixSince.UTC().Format(time.RFC3339))
		out = append(out, Violation{Key: text, Text: text})
	}
	return out
}

// CheckSummary condenses checks to none, green, red or pending; all-skipped
// checks are draft-phase leftovers and stay pending.
func CheckSummary(checks []Check) string {
	checks = latestChecks(checks)
	if len(checks) == 0 {
		return "none"
	}
	green, passed := true, false
	for _, c := range checks {
		switch c.Result {
		case "FAILURE", "CANCELLED", "TIMED_OUT", "ERROR", "ACTION_REQUIRED", "STARTUP_FAILURE":
			return "red"
		case "SUCCESS", "NEUTRAL":
			passed = true
		case "SKIPPED":
		default:
			green = false
		}
	}
	if green && passed {
		return "green"
	}
	return "pending"
}

func minutes(d time.Duration) int { return int(d / time.Minute) }

// staleSkipWindow is how long after a ready flip a draft-phase skip is
// treated as a result the new runs have not replaced yet.
const staleSkipWindow = 10 * time.Minute

// PRCheckSummary is CheckSummary, held pending while a skip predating the
// ready flip may still be replaced by a new run.
func PRCheckSummary(pr PullRequest, now time.Time) string {
	summary := CheckSummary(pr.Checks)
	checks := latestChecks(pr.Checks)
	if summary != "green" || pr.ReadyAt.IsZero() || !now.Before(pr.ReadyAt.Add(staleSkipWindow)) {
		return summary
	}
	for _, c := range checks {
		if c.Result == "SKIPPED" && c.CompletedAt.Before(pr.ReadyAt) {
			return "pending"
		}
	}
	return summary
}

// latestChecks keeps the newest run of each named check: the rollup also
// lists superseded runs, and a cancelled or skipped one must not outvote a
// rerun. A run without a completion time is still running and counts newest.
func latestChecks(checks []Check) []Check {
	pos := map[string]int{}
	out := make([]Check, 0, len(checks))
	for _, c := range checks {
		i, seen := pos[c.Name]
		if c.Name == "" || !seen {
			if c.Name != "" {
				pos[c.Name] = len(out)
			}
			out = append(out, c)
			continue
		}
		prev := out[i].CompletedAt
		if c.CompletedAt.IsZero() || (!prev.IsZero() && c.CompletedAt.After(prev)) {
			out[i] = c
		}
	}
	return out
}
