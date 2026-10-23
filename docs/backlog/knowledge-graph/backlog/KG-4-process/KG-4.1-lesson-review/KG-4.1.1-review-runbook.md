# KG-4.1.1 · Task: Review queries and triage rule in the runbook

**Status:** ⬜ todo — [#564](https://github.com/petarnenov/ultra-factory/issues/564)

**Type:** documentation. **Estimate:** 0.25 pd. **Story:** KG-4.1.
**Order:** 7 of 8. **Depends on:** KG-3.1.1 (#560) (retire, ratings).
**Next:** KG-4.2.1 (#566).

## Goal

Add a "Lesson review" section to `deploy/knowledge-graph/README.md`.

## Contents

- Queries (Query API or Browser) for: most-served lessons with
  `helped/hurt/useful/wrong`; lessons rated `wrong`; near-duplicate lessons
  (vector similarity above a threshold within one project); lessons older
  than 60 days never served.
- Triage rule, in order:
  1. tooling cause → file an issue to fix the tool, then retire the lesson;
  2. one area of the code → keep it in the graph, or move it to that
     directory's `CLAUDE.md` when it must always be visible there;
  3. every task in the repository → a short rule in `AGENTS.md` (rare).
- Cadence: every two weeks; record the outcome as an issue comment.
- Shared-graph writes (retire) are done by the operator.

## Acceptance criteria

- [ ] Every query in the section runs against the live graph.

## Verification

Run each query once and paste the counts into the PR description.
