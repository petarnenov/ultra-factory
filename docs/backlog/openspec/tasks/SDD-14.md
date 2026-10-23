# SDD-14 Roll out required SDD gates and measure adoption

Issue: [#631](https://github.com/petarnenov/ultra-factory/issues/631).

## Outcome

Roll out required SDD gates and measure adoption. Part of #617, the OpenSpec adoption tracker.

## Scope and integration points

AGENTS.md linked guidance, contributor docs, GitHub branch rules and SDD tracker.

Blueprint: https://github.com/petarnenov/ultra-factory/blob/docs/openspec-sdd-blueprint/docs/backlog/openspec/blueprint.md
Owner policy: [decisions D00–D13](https://github.com/petarnenov/ultra-factory/blob/docs/openspec-sdd-blueprint/docs/backlog/openspec/decisions.md). Read before implementation; all changes require a full proposal and geo.architect review.

Audit baseline: `060dd3cdb` (2026-10-23). Reconcile with current dev before implementation.

## Dependencies and priority

Hard dependencies: #630 (SDD-13).
Priority: `medium`. Normal migration work, scheduled after its foundations.
Responsible role: Delivery owner; named assignee to be set when scheduled. Status: backlog.

## Acceptance criteria

- [ ] Apply D09 when hardware/access/evidence is unavailable: record the exact affected acceptance and owned follow-up, continue independent approved work, and distinguish verified delivery from outstanding checks. Do not claim full verification from missing evidence.
- [ ] Apply D08: accumulate performance/reliability observations during the first rollout window; do not block completion on newly invented numerical targets. Preserve existing required checks and report sample counts, failures and measurement limitations.
- [ ] Under D12, root activates required checks after approved pilot/readiness evidence without another owner confirmation. Verify target-branch workflows, permissions and existing protections before activation; capture before/after settings. Main activation must wait for any required owner-operated promotion. Verify required checks and current-head semantic review on dev/main; demonstrate rejection of missing declarations and invalid archive entries plus acceptance of a compliant change.
- [ ] Complete capability ownership and documentation migration index; retain historical designs as linked history.
- [ ] Measure PR declaration completeness, declared-requirement mapping completeness, semantic-review gaps, escaped regressions and cycle time over the first ten subsequent changes. Record denominators and review evidence; do not claim automated discovery of undeclared behavior.
- [ ] Require all PRs covered by full proposals and council decisions as well as classified, 100% of declared changed requirements mapped, semantic review accepted and all high-priority blockers resolved before declaring adoption complete. Document rollback to reporting mode as requiring explicit owner permission; no silent downgrade of enforcement is authorized.

## Verification and delivery

Use one issue per PR targeting dev from an isolated task worktree. Cite exact scenario/test evidence and final commit; run complete applicable existing CI jobs for implementation changes. Documentation tasks validate source claims, links and examples. Respect the changed-Go-package coverage policy; native/device checks need actual platform evidence. Report unavailable checks explicitly. Never execute publication or production deployment as validation. Completion requires verified dev integration, not a draft PR or task checkbox alone.
