# SDD-01 Initialize OpenSpec latest with a reproducible repository workflow

Issue: [#618](https://github.com/petarnenov/ultra-factory/issues/618).

## Outcome

Initialize OpenSpec latest with a reproducible repository workflow. Part of #617, the OpenSpec adoption tracker.

## Scope and integration points

package.json, package-lock.json, openspec/config.yaml, openspec/ and contributor setup.

Blueprint: https://github.com/petarnenov/ultra-factory/blob/docs/openspec-sdd-blueprint/docs/backlog/openspec/blueprint.md
Owner policy: [decisions D00–D13](https://github.com/petarnenov/ultra-factory/blob/docs/openspec-sdd-blueprint/docs/backlog/openspec/decisions.md). Read before implementation; all changes require a full proposal and geo.architect review.

Audit baseline: `060dd3cdb` (2026-10-23). Reconcile with current dev before implementation.

## Dependencies and priority

Hard dependencies: None.
Priority: `high`. Blocks the foundation or verification gates of this adoption batch.
Responsible role: Capability owner; named assignee to be set when scheduled. Status: backlog.

## Acceptance criteria

- [ ] Read decisions D00–D13 before work. Prepare the full bootstrap proposal and obtain geo.architect approval under D02/D03/D05 before installing or initializing OpenSpec; proposal authoring can use repository Markdown before the CLI exists. After setup, validate the artifacts with the resolved tool.
- [ ] Under D13, resolve the official @fission-ai/openspec npm latest tag at implementation setup; record resolution time, actual version, upstream source/release identity, Node requirements and lockfile integrity. Use the same resolved exact version locally and in CI; verify clean installation on the CI Node runtime.
- [ ] Initialize the standard spec-driven schema in an isolated worktree; inspect generated agent files so existing rules are preserved.
- [ ] Document tested CLI equivalents for create/status/instructions/validate/archive, supported agent profiles and tool upgrade procedure. Upgrade proposals must resolve latest again explicitly; no automatic per-CI-run version drift.
- [ ] Validate a real sample change and malformed fixture; prove no runtime or release dependency on OpenSpec.

## Verification and delivery

Use one issue per PR targeting dev from an isolated task worktree. Cite exact scenario/test evidence and final commit; run complete applicable existing CI jobs for implementation changes. Documentation tasks validate source claims, links and examples. Respect the changed-Go-package coverage policy; native/device checks need actual platform evidence. Report unavailable checks explicitly. Never execute publication or production deployment as validation. Completion requires verified dev integration, not a draft PR or task checkbox alone.
