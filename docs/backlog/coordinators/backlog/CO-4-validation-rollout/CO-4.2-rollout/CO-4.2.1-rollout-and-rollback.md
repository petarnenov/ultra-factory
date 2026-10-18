# CO-4.2.1 · Task: Document coordinator rollout and rollback

**Status:** ⬜ todo

**Type:** operations and documentation. **Estimate:** 1–2 pd. **Story:** CO-4.2.

## Goal

Document a staged rollout and a safe return to root-owned workers after the
end-to-end pilot passes.

## Context

- Operator documentation belongs in `deploy/railway/README.md` and
  `docs/railway/architecture.md` where relevant.
- Feature/config defaults must remain disabled until the pilot is approved.
- Worker reports may be pending on remote hosts during ownership changes.

## Subtasks

- [ ] Document prerequisites, supported topology, capacity defaults, and
  feature enablement for one pilot project.
- [ ] Document signals to monitor: per-process memory, hub memory headroom,
  coordinator liveness, capacity rejections, pending report age, and delivery
  errors.
- [ ] Document how to stop new coordinator spawns and reassign active workers
  to the root while preserving pending reports.
- [ ] Document rollback verification and the criteria to disable the pilot.
- [ ] Link the architecture decision, implementation status, and end-to-end
  evidence.

## Acceptance criteria

- [ ] A second operator can follow the runbook without tribal knowledge.
- [ ] Rollback does not delete coordinator sessions, worker sessions, or
  pending reports until ownership is confirmed at the root.
- [ ] No deployment or production mutation is performed as part of writing or
  reviewing the runbook.

## Verification

- Walk through enablement and rollback in an isolated test environment.
- Check links and commands in the edited documentation.

## Dependencies

CO-4.1.1.
