# SDD-16 Enforce archive completeness independently of OpenSpec

Issue: [#634](https://github.com/petarnenov/ultra-factory/issues/634).

## Outcome

Enforce archive completeness independently of OpenSpec. Part of #617, the OpenSpec adoption tracker.

## Scope and integration points

scripts/ proposed archive checker, OpenSpec archive mappings and negative fixtures.

Blueprint: https://github.com/petarnenov/ultra-factory/blob/docs/openspec-sdd-blueprint/docs/backlog/openspec/blueprint.md
Audit baseline: `060dd3cdb` (2026-10-23). Reconcile with current dev before implementation.

## Dependencies and priority

Hard dependencies: #626 (SDD-09).
Priority: `high`. Blocks the foundation or verification gates of this adoption batch.
Responsible role: Delivery owner; named assignee to be set when scheduled. Status: backlog.

## Acceptance criteria

- [ ] Apply D10: root-only autonomous merge into dev requires proposal approval, independent review and successful applicable CI on the final head with no unresolved blocking review; invalidate readiness on head changes. No worker/coordinator merge or implicit main/deployment authority.
- [ ] Implement a deterministic AO check of added or modified archive entries, including direct filesystem moves that bypass the CLI. Incomplete task checklists, missing required mappings and absent verification references must fail.
- [ ] Require each archived change to identify its own implementation issue and PR; other prerequisite implementation PRs must already be merged into dev. The containing PR may prepare its own archive, but its exact final head must pass CI and semantic review before merge. Never require a PR to be merged before it can validate itself.
- [ ] Keep execution results external to the commit being checked: local checks validate the manifest, CI and review validate evidence for the final head, and the integrator records the actual dev merge afterward. Stale evidence must not authorize the current head; unavailable external evidence blocks readiness rather than counting as passed.
- [ ] Test incomplete tasks, missing evidence, stale revision, unmet prerequisite PRs, valid final-PR archival, repeated checks and direct archive moves. Test and document that CLI confirmation/override flags cannot bypass the AO merge gate.
- [ ] Use one change per independently mergeable implementation PR; compare its delta with accepted specs and ensure synchronization occurs once. Test added, modified, removed and renamed requirements plus a second run without further mutation. The checker itself must be read-only.
- [ ] Document the supported pinned-CLI archive procedure and recovery from a failed check; SDD-10 wires this check into CI before SDD-14 enables enforcement.

## Verification and delivery

Use one issue per PR targeting dev from an isolated task worktree. Cite exact scenario/test evidence and final commit; run complete applicable existing CI jobs for implementation changes. Documentation tasks validate source claims, links and examples. Respect the changed-Go-package coverage policy; native/device checks need actual platform evidence. Report unavailable checks explicitly. Never execute publication or production deployment as validation. Completion requires verified dev integration, not a draft PR or task checkbox alone.
