# SDD-11 Connect release and distribution evidence to specifications

Issue: [#628](https://github.com/petarnenov/ultra-factory/issues/628).

## Outcome

Connect release and distribution evidence to specifications. Part of #617, the OpenSpec adoption tracker.

## Scope and integration points

docs/agents/distribution.md, frontend/docs/desktop-release.md, release workflow documentation.

Blueprint: https://github.com/petarnenov/ultra-factory/blob/docs/openspec-sdd-blueprint/docs/backlog/openspec/blueprint.md
Audit baseline: `060dd3cdb` (2026-10-23). Reconcile with current dev before implementation.

## Dependencies and priority

Hard dependencies: #620 (SDD-03), #621 (SDD-04), #622 (SDD-05), #623 (SDD-06), #627 (SDD-10).
Priority: `medium`. Normal migration work, scheduled after its foundations.
Responsible role: Delivery owner; named assignee to be set when scheduled. Status: backlog.

## Acceptance criteria

- [ ] Map promoted change IDs to dev/main commits, versions and immutable artifact digests.
- [ ] Define Windows/macOS/Linux install/update and mobile compatibility evidence with exact environment and revision.
- [ ] Preserve sole publisher, main release boundary, daemon prerelease isolation and macOS zip/update requirements.
- [ ] Walk through rollback/compatibility and archival timing using existing artifacts; perform no production publication for validation.

## Verification and delivery

Use one issue per PR targeting dev from an isolated task worktree. Cite exact scenario/test evidence and final commit; run complete applicable existing CI jobs for implementation changes. Documentation tasks validate source claims, links and examples. Respect the changed-Go-package coverage policy; native/device checks need actual platform evidence. Report unavailable checks explicitly. Never execute publication or production deployment as validation. Completion requires verified dev integration, not a draft PR or task checkbox alone.
