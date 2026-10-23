# SDD-15 Run an early SDD pilot on one bounded capability

Issue: [#633](https://github.com/petarnenov/ultra-factory/issues/633).

## Outcome

Run an early SDD pilot on one bounded capability. Part of #617, the OpenSpec adoption tracker.

## Scope and integration points

Repository-local OpenSpec artifacts, contributor guidance and a linked bounded implementation issue.

Blueprint: https://github.com/petarnenov/ultra-factory/blob/docs/openspec-sdd-blueprint/docs/backlog/openspec/blueprint.md
Audit baseline: `060dd3cdb` (2026-10-23). Reconcile with current dev before implementation.

## Dependencies and priority

Hard dependencies: #619 (SDD-02).
Priority: `high`. Blocks the foundation or verification gates of this adoption batch.
Responsible role: Delivery owner; named assignee to be set when scheduled. Status: backlog.

## Acceptance criteria

- [ ] Under D07, agents select and execute the pilot cases without a separate owner selection gate; each change still requires its full proposal and council approval under D05. geo.architect decides final go/no-go from actual evidence using D03 arbitration.
- [ ] Select one small real behavior change after SDD-01 and SDD-02; seed only its capability from current code and tests. Do not require the repository-wide baseline, new CI gates or global agent integration.
- [ ] Use one child implementation issue, one OpenSpec change and one implementation PR; this pilot issue owns the evaluation report, not a second implementation of that change.
- [ ] Exercise proposal ratification through geo.architect under D02, declared impact, scenarios, implementation, semantic review, manual evidence checks, spec synchronization and archive on the pinned version. Manually reject incomplete artifacts until SDD-16 supplies enforcement.
- [ ] Record awkward fields, review effort, missing evidence and tool behavior; revise the initial conventions with the pilot results before SDD-03 expands them. Failures must become resolved blockers or explicitly owned follow-ups.
- [ ] Publish the selected issue/PR, accepted spec and reviewed commit evidence plus a go/no-go decision for broad migration. Do not publish a production release to run this pilot.

## Verification and delivery

Use one issue per PR targeting dev from an isolated task worktree. Cite exact scenario/test evidence and final commit; run complete applicable existing CI jobs for implementation changes. Documentation tasks validate source claims, links and examples. Respect the changed-Go-package coverage policy; native/device checks need actual platform evidence. Report unavailable checks explicitly. Never execute publication or production deployment as validation. Completion requires verified dev integration, not a draft PR or task checkbox alone.
