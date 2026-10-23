# SDD-10 Integrate OpenSpec and traceability checks into CI

Issue: [#627](https://github.com/petarnenov/ultra-factory/issues/627).

## Outcome

Integrate OpenSpec and traceability checks into CI. Part of #617, the OpenSpec adoption tracker.

## Scope and integration points

.github/workflows/, scripts/ci-check.mjs, package.json.

Blueprint: https://github.com/petarnenov/ultra-factory/blob/docs/openspec-sdd-blueprint/docs/backlog/openspec/blueprint.md
Audit baseline: `060dd3cdb` (2026-10-23). Reconcile with current dev before implementation.

## Dependencies and priority

Hard dependencies: #618 (SDD-01), #624 (SDD-07), #626 (SDD-09), #634 (SDD-16).
Priority: `high`. Blocks the foundation or verification gates of this adoption batch.
Responsible role: Delivery owner; named assignee to be set when scheduled. Status: backlog.

## Acceptance criteria

- [ ] Run pinned noninteractive OpenSpec, PR declaration/trace validation and the SDD-16 archive check, including changes to validators and direct archive moves. Require full-proposal and council-approval references for every change type under D05, including automated and promotion PRs. Evaluate every PR declaration; a code-only diff with no openspec path change must not bypass the check.
- [ ] Preserve API/sqlc drift, issue/comment policy, complete applicable backend/frontend/mobile/cloud suites and secret checks.
- [ ] Test draft-to-ready, changed heads, empty/deleted specs, forks, promotion and automated PR declarations, and unavailable credentials. Never count skipped/cancelled jobs or unavailable evidence as verified.
- [ ] Keep semantic review approval for the current head separate from structural CI. Require readiness/merge evidence on that head; avoid a check waiting for its own success or its own PR to be merged.
- [ ] Begin reporting-only; document exact local commands and diagnostic outputs. Missing archive evidence must be reported as invalid even during reporting mode; required branch-setting activation belongs to SDD-14.

## Verification and delivery

Use one issue per PR targeting dev from an isolated task worktree. Cite exact scenario/test evidence and final commit; run complete applicable existing CI jobs for implementation changes. Documentation tasks validate source claims, links and examples. Respect the changed-Go-package coverage policy; native/device checks need actual platform evidence. Report unavailable checks explicitly. Never execute publication or production deployment as validation. Completion requires verified dev integration, not a draft PR or task checkbox alone.
