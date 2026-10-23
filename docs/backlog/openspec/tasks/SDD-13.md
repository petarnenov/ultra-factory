# SDD-13 Pilot SDD on a feature, bugfix and operational change

Issue: [#630](https://github.com/petarnenov/ultra-factory/issues/630).

## Outcome

Pilot SDD on a feature, bugfix and operational change. Part of #617, the OpenSpec adoption tracker.

## Scope and integration points

docs/backlog/openspec/ pilot evidence, selected real change folders and linked implementation issues.

Blueprint: https://github.com/petarnenov/ultra-factory/blob/docs/openspec-sdd-blueprint/docs/backlog/openspec/blueprint.md
Audit baseline: `060dd3cdb` (2026-10-23). Reconcile with current dev before implementation.

## Dependencies and priority

Hard dependencies: #621 (SDD-04), #622 (SDD-05), #623 (SDD-06), #624 (SDD-07), #625 (SDD-08), #627 (SDD-10), #628 (SDD-11), #629 (SDD-12).
Priority: `medium`. Normal migration work, scheduled after its foundations.
Responsible role: Capability owner; named assignee to be set when scheduled. Status: backlog.

## Acceptance criteria

- [ ] Select three bounded real changes with individual issues/PRs; collectively exercise backend plus client, regression and operations paths.
- [ ] Capture proposal-to-review-to-dev evidence and release readiness, including tests for rejection/recovery and current-head CI.
- [ ] Exercise concurrent delta reconciliation and incomplete archive refusal; include cloud/mobile applicability or an explicit owned follow-up.
- [ ] Publish gaps, time/rework observations and rollout recommendation; do not close the umbrella tracker on pilot success alone.

## Verification and delivery

Use one issue per PR targeting dev from an isolated task worktree. Cite exact scenario/test evidence and final commit; run complete applicable existing CI jobs for implementation changes. Documentation tasks validate source claims, links and examples. Respect the changed-Go-package coverage policy; native/device checks need actual platform evidence. Report unavailable checks explicitly. Never execute publication or production deployment as validation. Completion requires verified dev integration, not a draft PR or task checkbox alone.
