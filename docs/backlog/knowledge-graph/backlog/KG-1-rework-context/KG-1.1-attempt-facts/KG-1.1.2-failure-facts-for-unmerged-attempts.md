# KG-1.1.2 · Task: CI and human review failure facts for unmerged attempts

**Status:** ⬜ todo — [#554](https://github.com/petarnenov/ultra-factory/issues/554)

**Type:** enhancement (daemon). **Estimate:** 1 pd. **Story:** KG-1.1.
**Order:** 3 of 8. **Depends on:** KG-1.1.1 (#553) (attempt line carries PR facts).
**Next:** KG-2.1.1 (#557).

## Goal

When a task is recorded with an outcome other than `merged`, store a short,
deterministic list of what blocked its PR, and show it on that attempt.

## Context (verified 2026-10-23)

- The daemon's SQLite holds the evidence, reachable through the store
  (`backend/internal/storage/sqlite/store/pr_store.go`):
  `ListChecks(prURL)` → `domain.PullRequestCheck{Name, Status, Conclusion, LogTail}`,
  `ListPRComments(prURL)` → `domain.PullRequestComment{Author, File, Line, Body, Resolved, IsBot}`,
  `ListPRReviews(prURL)`.
- `PRFacts.ExternalChangesRequested` marks a human changes-requested verdict.
- `knowledgegraph.Service` reads sessions and task reports through small ports
  (`service.go` `Sessions`, `TaskReports`); the daemon wires them in
  `backend/internal/daemon/knowledge_wiring.go`.

## Subtasks

- [ ] Add a port in `knowledgegraph` (e.g. `PRFailures`) returning, per PR URL:
  failed check names with a clipped log tail, and unresolved non-bot review
  comments (author, file, clipped body). Implement it in the daemon over the
  store; keep SQL and domain mapping out of `knowledgegraph`.
- [ ] In `factFor`, for outcomes other than `merged` and `in_progress`,
  collect at most 3 failed checks and 3 comments across the session's PRs;
  store them as `t.failures` (list of strings).
- [ ] Return `t.failures` from `attemptsCypher`; render under the attempt as
  `- blocked by: …`, each item clipped (e.g. 200 characters).
- [ ] Unit tests for selection (bots excluded, resolved excluded, caps),
  clipping and rendering.

## Acceptance criteria

- [ ] An unmerged attempt with a failed check shows the check name and a
  clipped tail; one with open human review comments shows them.
- [ ] Merged attempts store no failures.
- [ ] The section stays under `maxSectionText`; text sits after
  `SectionTrustBoundary`.

## Verification

Unit tests; live test as in KG-1.1.1; one probe respawn on an issue whose
earlier attempt failed CI shows the facts in its prompt.

## Rollout

Daemon change; part of the shared rollout.
