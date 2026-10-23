# SDD-13 Run the broad SDD pilot across feature, bugfix and operations

Issue: [#630](https://github.com/petarnenov/ultra-factory/issues/630).

## Outcome

Run the broad SDD pilot across feature, bugfix and operations. Part of #617, the OpenSpec adoption tracker.

## Scope and integration points

docs/backlog/openspec/ pilot evidence, selected real change folders and linked implementation issues.

Blueprint: https://github.com/petarnenov/ultra-factory/blob/docs/openspec-sdd-blueprint/docs/backlog/openspec/blueprint.md
Audit baseline: `060dd3cdb` (2026-10-23). Reconcile with current dev before implementation.

## Dependencies and priority

Hard dependencies: #621 (SDD-04), #622 (SDD-05), #623 (SDD-06), #624 (SDD-07), #625 (SDD-08), #627 (SDD-10), #628 (SDD-11), #629 (SDD-12), #633 (SDD-15), #634 (SDD-16).
Priority: `medium`. Normal migration work, scheduled after its foundations.
Responsible role: Capability owner; named assignee to be set when scheduled. Status: backlog.

## Acceptance criteria

- [ ] Apply D09 when hardware/access/evidence is unavailable: record the exact affected acceptance and owned follow-up, continue independent approved work, and distinguish verified delivery from outstanding checks. Do not claim full verification from missing evidence.
- [ ] Under D07, agents select and execute the pilot cases without a separate owner selection gate; each change still requires its full proposal and council approval under D05. geo.architect decides final go/no-go from actual evidence using D03 arbitration.
- [ ] Include all D04 platforms in broad-pilot applicability and evidence accounting. Unrun native checks stay explicitly unverified and cannot be silently dropped to pass the pilot.
- [ ] Build on SDD-15 early pilot evidence and select bounded real feature, bugfix and operational changes with individual issues, changes and implementation PRs. This issue owns their cross-surface evaluation report.
- [ ] Capture declaration-to-review-to-dev evidence and release readiness, including rejection/recovery scenarios and current-head CI.
- [ ] Exercise concurrent independent change reconciliation and the SDD-16 archive guard, including incomplete tasks, stale evidence, direct archive moves and CLI override attempts. Include Railway/self-hosted and mobile applicability or an explicit owned follow-up.
- [ ] Validate repository-local agent guidance with an unrelated-project control; confirm no global OpenSpec requirement is introduced.
- [ ] Publish gaps, time/rework observations and rollout recommendation; do not close the umbrella tracker on pilot success alone.

## Verification and delivery

Use one issue per PR targeting dev from an isolated task worktree. Cite exact scenario/test evidence and final commit; run complete applicable existing CI jobs for implementation changes. Documentation tasks validate source claims, links and examples. Respect the changed-Go-package coverage policy; native/device checks need actual platform evidence. Report unavailable checks explicitly. Never execute publication or production deployment as validation. Completion requires verified dev integration, not a draft PR or task checkbox alone.
