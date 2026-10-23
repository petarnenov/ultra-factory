# SDD-08 Integrate repository-local specification context with agent assignments

Issue: [#625](https://github.com/petarnenov/ultra-factory/issues/625).

## Outcome

Integrate repository-local specification context with agent assignments. Part of #617, the OpenSpec adoption tracker.

## Scope and integration points

AGENTS.md pointers, repository-local .agents/ instructions and docs/agents/ assignment and review guidance. Do not modify backend/internal/session_manager/prompt.go, embedded backend/internal/skillassets/ or other global AO prompts in this task.

Blueprint: https://github.com/petarnenov/ultra-factory/blob/docs/openspec-sdd-blueprint/docs/backlog/openspec/blueprint.md
Audit baseline: `060dd3cdb` (2026-10-23). Reconcile with current dev before implementation.

## Dependencies and priority

Hard dependencies: #619 (SDD-02), #620 (SDD-03).
Priority: `medium`. Normal migration work, scheduled after its foundations.
Responsible role: Capability owner; named assignee to be set when scheduled. Status: backlog.

## Acceptance criteria

- [ ] Apply D10: root-only autonomous merge into dev requires proposal approval, independent review and successful applicable CI on the final head with no unresolved blocking review; invalidate readiness on head changes. No worker/coordinator merge or implicit main/deployment authority.
- [ ] Use repository-local instructions and assignment templates to carry issue/change IDs, relevant capabilities, bounded task scope and scenario evidence for ultra-factory.
- [ ] Keep the AO product and other projects unchanged. If runtime integration later proves necessary, open a separately reviewed proposal with explicit per-project opt-in, default-off behavior and tests for projects without OpenSpec; this task does not implement it.
- [ ] Keep GitHub status authoritative; do not assume tracker intake can write issues or add a parallel persisted status model.
- [ ] Demonstrate one real assignment-to-review flow and resume/handoff using repository-local references. Reconcile conflicting spec edits by rebasing or serializing accepted changes.
- [ ] Confirm the delivered diff contains no global prompt or embedded skill changes; verify a control assignment for a project without OpenSpec acquires no SDD requirement from this change.

## Verification and delivery

Use one issue per PR targeting dev from an isolated task worktree. Cite exact scenario/test evidence and final commit; run complete applicable existing CI jobs for implementation changes. Documentation tasks validate source claims, links and examples. Respect the changed-Go-package coverage policy; native/device checks need actual platform evidence. Report unavailable checks explicitly. Never execute publication or production deployment as validation. Completion requires verified dev integration, not a draft PR or task checkbox alone.
