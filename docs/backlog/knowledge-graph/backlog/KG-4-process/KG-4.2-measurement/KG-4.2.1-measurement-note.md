# KG-4.2.1 · Task: Measurement note and decay decision

**Status:** ⛔ blocked until about 50 recorded tasks have an outcome — [#566](https://github.com/petarnenov/ultra-factory/issues/566)

**Type:** chore (measurement). **Estimate:** 0.5 pd. **Story:** KG-4.2.
**Order:** 8 of 8. **Depends on:** KG-1.1.2 (#554), KG-2.1.1 (#557) rolled out ≥ 2 weeks;
≥ 50 tasks with an outcome. **Next:** none.

## Goal

A short note under `docs/performance/knowledge-graph/` with numbers from data
already stored, a check of how write-time merging (ADR 0014) behaves,
and a decision on time decay.

## Context (verified 2026-10-23)

Each task already stores `outcome`, `prs`, `elapsedSeconds`, input/output
tokens and `stages` (`graph.go` `RecordTask`); attempts per issue come from
`FOR_ISSUE`; served lessons from `RECEIVED`.

Since #568 (ADR [0014](../../../../../adr/0014-merge-near-identical-lessons.md)),
`AddLesson` merges a near-identical lesson of the same project and kind
(score ≥ `mergeScore`, default 0.94, plus a lexical guard) into the stored
one: the lesson gets `confirmations` and `confirmedAt`, and the confirming
task a `CONFIRMED {at, texts}` edge. Lessons stored before #568 are not
merged, and `confirmations` does not affect ranking.

## Subtasks

- [ ] Queries: attempts until merge per issue; elapsed time and tokens per
  merged task; CI-failure share; tasks with vs without matched prior context
  (any served lesson or attempt).
- [ ] State the limits: no control group, small sample, mixed task sizes.
- [ ] Merge check: count `CONFIRMED` edges and lessons with
  `confirmations > 0`; read every `CONFIRMED.texts` for a wrong merge (a
  contradiction that passed the guard); list remaining duplicates among
  lessons stored before #568.
- [ ] Decide: whether `confirmations` should enter the rank, whether the
  pre-#568 duplicates need a one-off merge, and time decay or not; file
  follow-up issues for what is chosen.

## Acceptance criteria

- [ ] The note gives each number with its query and sample size.
- [ ] Every wrong merge found is listed with its lesson id.
