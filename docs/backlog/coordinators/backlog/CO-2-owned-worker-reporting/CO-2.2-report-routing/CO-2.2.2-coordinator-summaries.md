# CO-2.2.2 · Task: Deliver coordinator summaries and escalations to root

**Status:** ⬜ todo

**Type:** backend and orchestration contract. **Estimate:** 1 pd.
**Story:** CO-2.2.

## Goal

Give coordinators a bounded way to send status summaries, completed outcomes,
and escalations to the root without forwarding every raw worker report.

## Context

- Reports are durable worker outcomes. Coordinator summaries should be
  distinguishable from worker reports and link back to the assigned workers.
- The root retains project-wide ownership and final integration authority.
- Use the summary schema and delivery semantics approved in CO-1.2.1.

## Subtasks

- [ ] Define a bounded summary payload with coordinator, task scope, worker
  references, state, result, and escalation reason.
- [ ] Persist and deliver summaries idempotently to the root.
- [ ] Expose summary state in the daemon API for operator inspection.
- [ ] Add tests for duplicate submission, root restart, and coordinator
  termination after submission.

## Acceptance criteria

- [ ] The root can inspect each coordinator's current state and latest
  accepted summary.
- [ ] Summaries do not replace the underlying worker report record or erase
  audit history.
- [ ] Only the root receives project-wide escalations and makes integration
  decisions.

## Verification

- Focused service and controller tests for durable summary delivery and
  idempotency.
- Confirm the frontend/API contract if the operator UI consumes summaries.

## Dependencies

CO-2.1.1, CO-2.1.2, CO-2.2.1.
