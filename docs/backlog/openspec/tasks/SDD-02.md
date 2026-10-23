# SDD-02 Define SDD intake, review and source of truth policy

Issue: [#619](https://github.com/petarnenov/ultra-factory/issues/619).

## Outcome

Define SDD intake, review and source of truth policy. Part of #617, the OpenSpec adoption tracker.

## Scope and integration points

AGENTS.md pointers, docs/agents/, docs/documentation-map.md, issue forms and PR template.

Blueprint: https://github.com/petarnenov/ultra-factory/blob/docs/openspec-sdd-blueprint/docs/backlog/openspec/blueprint.md
Owner policy: [decisions D00–D13](https://github.com/petarnenov/ultra-factory/blob/docs/openspec-sdd-blueprint/docs/backlog/openspec/decisions.md). Read before implementation; all changes require a full proposal and geo.architect review.

Audit baseline: `060dd3cdb` (2026-10-23). Reconcile with current dev before implementation.

## Dependencies and priority

Hard dependencies: #618 (SDD-01).
Priority: `high`. Blocks the foundation or verification gates of this adoption batch.
Responsible role: Capability owner; named assignee to be set when scheduled. Status: backlog.

## Acceptance criteria

- [ ] Apply D10: root-only autonomous merge into dev requires proposal approval, independent review and successful applicable CI on the final head with no unresolved blocking review; invalidate readiness on head changes. No worker/coordinator merge or implicit main/deployment authority.
- [ ] Translate confirmed decisions D00–D13 into repository-local enforceable instructions without reopening the interview. Apply D03 as an explicit initiative-specific override to the council fallback behavior; do not alter the global skill for unrelated projects.
- [ ] Require geo.architect review for every full change proposal under D02 and the D03 final-arbiter order: Fable 5.1 → Codex Astra → OpenCode DeepSeek Flash 4.1. Link explicit rulings to the evaluated proposal/revision and archive; test all fallback cases and the all-unavailable blocked outcome. Never infer approval from missing votes.
- [ ] Define readiness/completion, roles, stable requirement/scenario IDs and one OpenSpec change per independently mergeable implementation issue/PR. Keep umbrella planning in GitHub; do not retain an umbrella delta that is reapplied across child PRs.
- [ ] Require each PR to reference a full proposal/change ID and declare affected capabilities with a justified no-runtime-behavior classification where applicable. Require a reviewer to check the declaration against the diff and record the reviewed head; machine validation cannot discover undeclared semantics.
- [ ] Preserve one issue per PR and exactly one type/priority; distinguish accepted dev specs from released behavior. Describe promotion, automation and docs-only declarations explicitly rather than adding silent exemptions.
- [ ] Require full proposals and council review for all changes under D05, including documentation, formatting, automated changes and emergencies. No-behavior classifications never bypass approval; define meaningful non-runtime acceptance rather than inventing behavior. No deferred-proposal exception is authorized.
- [ ] Delegate code/test/documentation contradictions to geo.architect under D06/D03 and record evidence-based intended behavior without weakening hard requirements. Document conflict resolution with ADRs/generated contracts, final-PR synchronization/archive and source evidence. Walk through feature, bugfix and maintenance examples; SDD-15 must refine these conventions before broad baselining.

## Verification and delivery

Use one issue per PR targeting dev from an isolated task worktree. Cite exact scenario/test evidence and final commit; run complete applicable existing CI jobs for implementation changes. Documentation tasks validate source claims, links and examples. Respect the changed-Go-package coverage policy; native/device checks need actual platform evidence. Report unavailable checks explicitly. Never execute publication or production deployment as validation. Completion requires verified dev integration, not a draft PR or task checkbox alone.
