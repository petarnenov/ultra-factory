# CO-3.2.1 · Task: Define coordinator operating prompt and handoff

**Status:** ⬜ todo

**Type:** orchestration and documentation. **Estimate:** 1–2 pd.
**Story:** CO-3.2.

## Goal

Provide a reusable operating contract that keeps helper coordinators focused
on their assigned scope and gives the root useful, compact updates.

## Context

- The current Railway operating prompt is
  `docs/backlog/railway/orchestrator-prompt.md`.
- Reports use the AO report service and remote report pull path; the structured
  coordinator summary is defined in CO-2.2.2.
- All repository documentation must be in English.

## Subtasks

- [ ] Define the coordinator prompt template and required scope, task list,
  worker cap, branch rules, and reporting cadence.
- [ ] Require status summaries on assignment, meaningful state changes,
  completion, and blockers.
- [ ] Require escalation for overlapping files, unclear acceptance criteria,
  failed workers, and decisions outside the assigned scope.
- [ ] Prohibit final merges and project-wide issue closure by coordinators.
- [ ] Document root reassignment and coordinator replacement while preserving
  worker and report ownership.

## Acceptance criteria

- [ ] A coordinator can manage three workers without receiving unrelated
  project tasks.
- [ ] The root gets summaries rather than routine raw worker conversation.
- [ ] Prompt instructions distinguish a worker completion from an integrated
  project completion.
- [ ] The contract can be exercised in the end-to-end pilot without manual
  report forwarding.

## Verification

- Review the prompt against the ownership, report, capacity, and integration
  invariants in the backlog README.
- Exercise one successful worker and one blocked worker handoff in CO-4.1.1.

## Dependencies

CO-2.2.2, CO-3.1.1.
