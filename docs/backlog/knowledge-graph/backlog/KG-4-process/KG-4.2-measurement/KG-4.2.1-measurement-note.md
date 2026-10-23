# KG-4.2.1 · Task: Measurement note and dedup/decay decision

**Status:** ⛔ blocked until about 50 recorded tasks have an outcome — [#566](https://github.com/petarnenov/ultra-factory/issues/566)

**Type:** chore (measurement). **Estimate:** 0.5 pd. **Story:** KG-4.2.
**Order:** 8 of 8. **Depends on:** KG-1.1.2 (#554), KG-2.1.1 (#557) rolled out ≥ 2 weeks;
≥ 50 tasks with an outcome. **Next:** none.

## Goal

A short note under `docs/performance/knowledge-graph/` with numbers from data
already stored, and a decision on dedup and decay.

## Context (verified 2026-10-23)

Each task already stores `outcome`, `prs`, `elapsedSeconds`, input/output
tokens and `stages` (`graph.go` `RecordTask`); attempts per issue come from
`FOR_ISSUE`; served lessons from `RECEIVED`.

## Subtasks

- [ ] Queries: attempts until merge per issue; elapsed time and tokens per
  merged task; CI-failure share; tasks with vs without matched prior context
  (any served lesson or attempt).
- [ ] State the limits: no control group, small sample, mixed task sizes.
- [ ] Decide: dedup near-identical lessons, time decay, or neither; file
  follow-up issues for what is chosen.

## Acceptance criteria

- [ ] The note gives each number with its query and sample size.
