# SDD-08 Integrate specification context with agent assignments and review

Issue: [#625](https://github.com/petarnenov/ultra-factory/issues/625).

## Outcome

Integrate specification context with agent assignments and review. Part of #617, the OpenSpec adoption tracker.

## Scope and integration points

backend/internal/session_manager/prompt.go, backend/internal/skillassets/, docs/coordinators/operating-contract.md.

Blueprint: https://github.com/petarnenov/ultra-factory/blob/docs/openspec-sdd-blueprint/docs/backlog/openspec/blueprint.md
Audit baseline: `060dd3cdb` (2026-10-23). Reconcile with current dev before implementation.

## Dependencies and priority

Hard dependencies: #619 (SDD-02), #620 (SDD-03).
Priority: `medium`. Normal migration work, scheduled after its foundations.
Responsible role: Capability owner; named assignee to be set when scheduled. Status: backlog.

## Acceptance criteria

- [ ] Include issue/change IDs, relevant capabilities, bounded task scope and scenario evidence in assignment/review guidance.
- [ ] Keep GitHub status authoritative; do not assume tracker intake can write issues or add a parallel persisted status model.
- [ ] Verify resume/handoff preserves references and conflicting spec edits are escalated or serialized.
- [ ] Test prompt/instruction integration, then demonstrate one real assignment-to-review flow; fixtures alone do not prove agent behavior.

## Verification and delivery

Use one issue per PR targeting dev from an isolated task worktree. Cite exact scenario/test evidence and final commit; run complete applicable existing CI jobs for implementation changes. Documentation tasks validate source claims, links and examples. Respect the changed-Go-package coverage policy; native/device checks need actual platform evidence. Report unavailable checks explicitly. Never execute publication or production deployment as validation. Completion requires verified dev integration, not a draft PR or task checkbox alone.
