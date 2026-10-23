# SDD-07 Define security, privacy and nonfunctional scenarios

Issue: [#624](https://github.com/petarnenov/ultra-factory/issues/624).

## Outcome

Define security, privacy and nonfunctional scenarios. Part of #617, the OpenSpec adoption tracker.

## Scope and integration points

openspec/specs/, docs/agents/network-boundaries.md, docs/railway/threat-model.md, docs/telemetry.md.

Blueprint: https://github.com/petarnenov/ultra-factory/blob/docs/openspec-sdd-blueprint/docs/backlog/openspec/blueprint.md
Audit baseline: `060dd3cdb` (2026-10-23). Reconcile with current dev before implementation.

## Dependencies and priority

Hard dependencies: #619 (SDD-02), #620 (SDD-03).
Priority: `high`. Blocks the foundation or verification gates of this adoption batch.
Responsible role: Capability owner; named assignee to be set when scheduled. Status: backlog.

## Acceptance criteria

- [ ] Map loopback/remote listener boundaries, identity exemption, auth failures, host/tenant isolation and secret handling to negative tests.
- [ ] Cover AO state path confinement and browser-import restrictions, telemetry privacy and dependency/security scanning.
- [ ] Define performance/reliability acceptance templates with workload, environment, metric and owner-approved thresholds.
- [ ] Review scenarios against current ADRs and record gaps without weakening boundaries or inventing measured SLOs.

## Verification and delivery

Use one issue per PR targeting dev from an isolated task worktree. Cite exact scenario/test evidence and final commit; run complete applicable existing CI jobs for implementation changes. Documentation tasks validate source claims, links and examples. Respect the changed-Go-package coverage policy; native/device checks need actual platform evidence. Report unavailable checks explicitly. Never execute publication or production deployment as validation. Completion requires verified dev integration, not a draft PR or task checkbox alone.
