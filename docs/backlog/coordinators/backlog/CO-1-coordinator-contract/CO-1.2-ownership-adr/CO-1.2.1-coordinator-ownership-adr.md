# CO-1.2.1 · Task: ADR for coordinator hierarchy and report routing

**Status:** ⬜ todo

**Type:** architecture. **Estimate:** 1–2 pd. **Story:** CO-1.2.

## Goal

Record the domain contract for a root orchestrator, two helper coordinators,
and up to three workers per coordinator before changing persistence or APIs.

## Context

- `session.Service.SpawnOrchestrator` currently returns the active project
  orchestrator rather than creating a second one.
- `report.Coordinator.DeliverExternal` routes remote reports to the active
  orchestrator for the project.
- `remote_worker_links` stores the spawning parent session, but that value is
  not currently used to select the report recipient.
- Read `docs/architecture.md`,
  `docs/railway/architecture.md`, and
  `docs/backlog/coordinators/coordinator-report.md`.

## Subtasks

- [ ] Decide whether coordinator is a new session role or an explicitly
  constrained orchestrator subtype; document why.
- [ ] Define durable ownership for local and remote workers and the report
  recipient lookup rules.
- [ ] Define coordinator summaries and escalation messages to the root,
  including the source of truth for progress.
- [ ] Define atomic reassignment and recovery when a coordinator terminates.
- [ ] Define concurrency limits and how the daemon reports capacity errors.
- [ ] Define compatibility and migration behavior for existing links,
  sessions, and direct root-owned workers.
- [ ] Record security boundaries and confirm only the root can merge.

## Acceptance criteria

- [ ] The ADR includes state diagrams or explicit transition tables for
  assignment, report delivery, coordinator termination, and reassignment.
- [ ] The ADR specifies idempotency and retry behavior for report delivery.
- [ ] The ADR keeps exactly one project root orchestrator and preserves direct
  root-to-worker delegation.
- [ ] Implementation stories can be split into persistence/API, runtime, and
  rollout without unresolved ownership decisions.

## Verification

- Architecture review against current report coordinator, remote worker link,
  orchestrator spawn, and session lifecycle code.
- No runtime code changes in this task.

## Dependencies

CO-1.1.1.
