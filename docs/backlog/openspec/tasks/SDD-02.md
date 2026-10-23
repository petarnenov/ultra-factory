# SDD-02 Define SDD intake, review and source of truth policy

Issue: [#619](https://github.com/petarnenov/ultra-factory/issues/619).

## Outcome

Define SDD intake, review and source of truth policy. Part of #617, the OpenSpec adoption tracker.

## Scope and integration points

AGENTS.md pointers, docs/agents/, docs/documentation-map.md, issue forms and PR template.

Blueprint: https://github.com/petarnenov/ultra-factory/blob/docs/openspec-sdd-blueprint/docs/backlog/openspec/blueprint.md
Audit baseline: `060dd3cdb` (2026-10-23). Reconcile with current dev before implementation.

## Dependencies and priority

Hard dependencies: #618 (SDD-01).
Priority: `high`. Blocks the foundation or verification gates of this adoption batch.
Responsible role: Capability owner; named assignee to be set when scheduled. Status: backlog.

## Acceptance criteria

- [ ] Define readiness and completion criteria, roles, scope classification and requirement/scenario identity conventions.
- [ ] Preserve one issue per PR and exactly one type/priority; distinguish accepted dev specs from released behavior.
- [ ] Define proportional handling for docs-only and behavior-preserving maintenance plus owned, dated emergency follow-up.
- [ ] Document how contradictions with ADRs/generated contracts are reconciled; walkthrough feature, bugfix and maintenance examples.

## Verification and delivery

Use one issue per PR targeting dev from an isolated task worktree. Cite exact scenario/test evidence and final commit; run complete applicable existing CI jobs for implementation changes. Documentation tasks validate source claims, links and examples. Respect the changed-Go-package coverage policy; native/device checks need actual platform evidence. Report unavailable checks explicitly. Never execute publication or production deployment as validation. Completion requires verified dev integration, not a draft PR or task checkbox alone.
