# SDD-09 Add requirement and scenario evidence validation

Issue: [#626](https://github.com/petarnenov/ultra-factory/issues/626).

## Outcome

Add requirement and scenario evidence validation. Part of #617, the OpenSpec adoption tracker.

## Scope and integration points

scripts/ proposed traceability checks, openspec/ verification mappings and test fixtures.

Blueprint: https://github.com/petarnenov/ultra-factory/blob/docs/openspec-sdd-blueprint/docs/backlog/openspec/blueprint.md
Audit baseline: `060dd3cdb` (2026-10-23). Reconcile with current dev before implementation.

## Dependencies and priority

Hard dependencies: #619 (SDD-02), #620 (SDD-03).
Priority: `high`. Blocks the foundation or verification gates of this adoption batch.
Responsible role: Capability owner; named assignee to be set when scheduled. Status: backlog.

## Acceptance criteria

- [ ] Define a minimal machine-readable mapping for requirement/scenario IDs, test path/name, platform and manual evidence references.
- [ ] Reject duplicate/missing IDs, dangling mappings, unmapped changed behavior and stale mappings after rename/deletion.
- [ ] Test positive and negative fixtures, multi-capability changes, no-behavior declarations and explicit manual evidence.
- [ ] Report structural validation separately from test execution; no fabricated test pass from a file existing.

## Verification and delivery

Use one issue per PR targeting dev from an isolated task worktree. Cite exact scenario/test evidence and final commit; run complete applicable existing CI jobs for implementation changes. Documentation tasks validate source claims, links and examples. Respect the changed-Go-package coverage policy; native/device checks need actual platform evidence. Report unavailable checks explicitly. Never execute publication or production deployment as validation. Completion requires verified dev integration, not a draft PR or task checkbox alone.
