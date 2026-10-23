# SDD-09 Add requirement and scenario evidence validation

Issue: [#626](https://github.com/petarnenov/ultra-factory/issues/626).

## Outcome

Add requirement and scenario evidence validation. Part of #617, the OpenSpec adoption tracker.

## Scope and integration points

scripts/ proposed traceability checks, openspec/ verification mappings and test fixtures.

Blueprint: https://github.com/petarnenov/ultra-factory/blob/docs/openspec-sdd-blueprint/docs/backlog/openspec/blueprint.md
Owner policy: [decisions D00–D13](https://github.com/petarnenov/ultra-factory/blob/docs/openspec-sdd-blueprint/docs/backlog/openspec/decisions.md). Read before implementation; all changes require a full proposal and geo.architect review.

Audit baseline: `060dd3cdb` (2026-10-23). Reconcile with current dev before implementation.

## Dependencies and priority

Hard dependencies: #619 (SDD-02), #620 (SDD-03).
Priority: `high`. Blocks the foundation or verification gates of this adoption batch.
Responsible role: Capability owner; named assignee to be set when scheduled. Status: backlog.

## Acceptance criteria

- [ ] Define the machine-readable PR impact declaration and mapping for declared capabilities/change IDs, requirement/scenario IDs, test path/name, platform and manual evidence references. Apply the early pilot findings.
- [ ] Reject missing full proposals or council decisions under D05 for any change type, including justified no-runtime-behavior work. Reject absent declarations, duplicate/missing IDs, dangling mappings, unmapped declared changed requirements and stale mappings after rename/deletion. Validate a no-behavior rationale structurally, not as proof that it is true.
- [ ] Require semantic review against the actual diff and reviewed head to catch undeclared behavior. Specify how review evidence becomes stale after a push; do not claim the deterministic checker can infer every behavioral change from code.
- [ ] Test behavior-code changes with no declaration, malformed or empty declarations, renamed/deleted mappings, multi-capability changes, explicit manual evidence and no-behavior declarations both with and without the required full proposal and approval. Include a misleading declaration example that only semantic review can reject.
- [ ] Report declaration completeness, mapping completeness and semantic review separately from test execution. Name the denominator in coverage metrics: 100% of declared changed requirements mapped is not 100% behavioral or code coverage.

## Verification and delivery

Use one issue per PR targeting dev from an isolated task worktree. Cite exact scenario/test evidence and final commit; run complete applicable existing CI jobs for implementation changes. Documentation tasks validate source claims, links and examples. Respect the changed-Go-package coverage policy; native/device checks need actual platform evidence. Report unavailable checks explicitly. Never execute publication or production deployment as validation. Completion requires verified dev integration, not a draft PR or task checkbox alone.
