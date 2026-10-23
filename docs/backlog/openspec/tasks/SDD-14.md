# SDD-14 Roll out required SDD gates and measure adoption

Issue: [#631](https://github.com/petarnenov/ultra-factory/issues/631).

## Outcome

Roll out required SDD gates and measure adoption. Part of #617, the OpenSpec adoption tracker.

## Scope and integration points

AGENTS.md linked guidance, contributor docs, GitHub branch rules and SDD tracker.

Blueprint: https://github.com/petarnenov/ultra-factory/blob/docs/openspec-sdd-blueprint/docs/backlog/openspec/blueprint.md
Audit baseline: `060dd3cdb` (2026-10-23). Reconcile with current dev before implementation.

## Dependencies and priority

Hard dependencies: #630 (SDD-13).
Priority: `medium`. Normal migration work, scheduled after its foundations.
Responsible role: Delivery owner; named assignee to be set when scheduled. Status: backlog.

## Acceptance criteria

- [ ] Verify maintainer-enabled required checks on dev/main and demonstrate an invalid change is rejected while a compliant change passes.
- [ ] Complete capability ownership and documentation migration index; retain historical designs as linked history.
- [ ] Measure changed-requirement evidence coverage, gaps, escaped regressions and cycle time over the first ten subsequent changes.
- [ ] Require all changed behavior mapped, all high-priority blockers resolved and rollback-to-reporting documented before declaring adoption complete.

## Verification and delivery

Use one issue per PR targeting dev from an isolated task worktree. Cite exact scenario/test evidence and final commit; run complete applicable existing CI jobs for implementation changes. Documentation tasks validate source claims, links and examples. Respect the changed-Go-package coverage policy; native/device checks need actual platform evidence. Report unavailable checks explicitly. Never execute publication or production deployment as validation. Completion requires verified dev integration, not a draft PR or task checkbox alone.
