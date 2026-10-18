# CO-3.1.1 · Task: Add coordinator spawn and capacity rules

**Status:** ⬜ todo

**Type:** backend and CLI. **Estimate:** 1–2 pd. **Story:** CO-3.1.

## Goal

Allow the root orchestrator to spawn two scoped coordinators and enforce the
initial coordinator and worker capacity limits in the daemon.

## Context

- Current orchestrator spawn behavior enforces one active orchestrator per
  project in `backend/internal/service/session/service.go`.
- Worker sessions are created through the session service and CLI spawn flow.
- The coordinator role and authorization contract are defined by CO-1.2.1.
- Limits must be enforced in the service layer so CLI and HTTP callers cannot
  bypass them.

## Subtasks

- [ ] Add a supported root-only operation for spawning a coordinator with a
  required scope and durable root ownership.
- [ ] Assign workers spawned from a coordinator to that coordinator.
- [ ] Enforce at most two active coordinators, three active owned workers per
  coordinator, and six active workers total per project.
- [ ] Return stable capacity errors with current and allowed counts.
- [ ] Define slot release for termination, failed startup, and reassignment.
- [ ] Add CLI support using daemon HTTP routes and table-driven validation
  tests.

## Acceptance criteria

- [ ] Capacity limits hold under concurrent spawn requests.
- [ ] A non-root session cannot create coordinators or exceed another
  coordinator's worker capacity.
- [ ] Rejected spawns create no session and consume no slot.
- [ ] Direct root-to-worker spawning still works within the project total.
- [ ] Error responses preserve existing API envelopes and CLI exit semantics.

## Verification

- Focused service and CLI tests for limits, authorization, concurrent spawn,
  failed startup, and slot release.
- `cd backend && go test ./internal/service/session/... ./internal/cli/... ./internal/httpd/...`.

## Dependencies

CO-2.1.2.
