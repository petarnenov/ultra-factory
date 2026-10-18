# CO-2.2.1 · Task: Route local and remote reports to worker owner

**Status:** ⬜ todo

**Type:** backend. **Estimate:** 1–2 pd. **Story:** CO-2.2.

## Goal

Deliver each worker's reports to its assigned coordinator, including reports
pulled from a paired remote AO host.

## Context

- Local delivery is coordinated by
  `backend/internal/service/report/coordinator.go`.
- Remote delivery is pulled by
  `backend/internal/observe/remotereports/observer.go`.
- Remote links retain the spawning parent, but current external delivery
  selects the active project orchestrator.
- Delivery acceptance, retry delay, and idempotency are correctness
  requirements; preserve the report observer's acknowledgement ordering.

## Subtasks

- [ ] Resolve a worker's durable owner at delivery time and verify it remains
  an eligible live coordinator.
- [ ] Route local reports and externally pulled reports through the same owner
  selection rule.
- [ ] Define behavior when the owner is unavailable or terminated; do not
  acknowledge a report before durable acceptance.
- [ ] Preserve idempotency across retry, owner restart, and reassignment.
- [ ] Add tests for local/remote routing, fallback, retry, and duplicate pulls.

## Acceptance criteria

- [ ] A worker report is delivered only to its assigned coordinator unless the
  ADR's recovery rule explicitly reassigns ownership.
- [ ] Legacy workers route to the root.
- [ ] A failed or unavailable recipient leaves the report recoverable.
- [ ] Existing exactly-once delivery guarantees are not weakened.

## Verification

- `cd backend && go test ./internal/service/report/... ./internal/observe/remotereports/... ./internal/httpd/controllers/...`.
- Run existing remote report integration tests and add owner-routing cases.

## Dependencies

CO-2.1.1, CO-2.1.2.
