# KG-1.1.1 · Task: PR URL and branch in earlier attempts

**Status:** ⬜ todo — [#553](https://github.com/petarnenov/ultra-factory/issues/553)

**Type:** enhancement (daemon). **Estimate:** 0.5 pd. **Story:** KG-1.1.
**Order:** 2 of 8. **Depends on:** none (KG-0.1.1 (#550) first by order only).
**Next:** KG-1.1.2 (#554).

## Goal

Each line under "Earlier attempts on this issue" names the attempt's PR URL(s)
and its branch.

## Context (verified 2026-10-23)

- `RecordTask` already stores `t.prs` (`backend/internal/knowledgegraph/graph.go`,
  `props["prs"]`), filled from `sess.PRs` in `recorder.go` `factFor`.
- No branch is stored. `domain.Session.Branch` and `PRFacts.SourceBranch`
  (`backend/internal/domain/pr.go`) carry it.
- `attemptsCypher` (`graph.go`) returns title, outcome, summary and lessons;
  `Item` has no PR or branch field; `format.go` `formatItem` renders attempts.
- Since #568 (ADR [0014](../../../../../adr/0014-merge-near-identical-lessons.md)),
  `attemptsCypher` collects lessons over `[:LEARNED|CONFIRMED]` with
  `collect(DISTINCT l.text)[..5]`. An attempt therefore also lists the lessons
  its task confirmed, i.e. lessons it tried to add that merged into a stored
  one. Keep both relationship types and `DISTINCT` when adding the PR and
  branch fields. Read `t.prs` and `t.branch` from `t`, which the
  `WITH t, …` aggregation already carries. Start from current `dev`.

## Subtasks

- [ ] Add `Branch` to `TaskFact`, set from `sess.Branch` in `factFor`, stored
  as `t.branch` via `setIf`.
- [ ] Return `coalesce(t.prs, [])` and `coalesce(t.branch, '')` from
  `attemptsCypher`; carry them on `Item` (e.g. `PRs []string`, `Branch string`).
- [ ] Render them on the attempt line in `formatItem`, clipped; omit when empty.
- [ ] Unit tests: `graph_test.go` (query parameters and row mapping),
  `format_test.go` (rendering with and without PRs and branch).

## Acceptance criteria

- [ ] An attempt with a PR renders `PR: <url> · branch: <branch>`.
- [ ] Attempts without either render as today.
- [ ] An attempt still lists both learned and confirmed lessons, without
  duplicates (`graph_test.go` covers the query text).
- [ ] Lesson and similar-task sections are unchanged.

## Verification

`go test ./internal/knowledgegraph/...`; `go test -tags kglive -run TestLive
./internal/knowledgegraph/` against the live graph without running the global
backfill; `make ci-check`.

## Rollout

Daemon change. Merge to `dev`; roll out together with KG-1.1.2, KG-2.1.1 and
KG-3.1.1 (see the backlog README).
