# SDD-06 Baseline Railway, self hosted workers and supported shared contracts

Issue: [#623](https://github.com/petarnenov/ultra-factory/issues/623).

## Outcome

Baseline Railway, self hosted workers and supported shared contracts. Part of #617, the OpenSpec adoption tracker.

## Scope and integration points

openspec/specs/, packages/{product-ui,hub-bff}/, deploy/railway/ and remote-worker contracts. AO Cloud public/private components and cloud-only clients/contracts are excluded by owner decision D00; Railway/self-hosted remains in scope under D01.

Blueprint: https://github.com/petarnenov/ultra-factory/blob/docs/openspec-sdd-blueprint/docs/backlog/openspec/blueprint.md
Audit baseline: `060dd3cdb` (2026-10-23). Reconcile with current dev before implementation.

## Dependencies and priority

Hard dependencies: #619 (SDD-02), #620 (SDD-03).
Priority: `medium`. Normal migration work, scheduled after its foundations.
Responsible role: Capability owner; named assignee to be set when scheduled. Status: backlog.

## Acceptance criteria

- [ ] Map Railway/self-hosted hub and worker lifecycle, shared UI/BFF boundaries and supported consumer compatibility to public code/test evidence.
- [ ] Cover BFF cookie/CSRF, REST/SSE/mux proxying, bearer handling, remote-worker isolation, drain and backup/restore scenarios.
- [ ] Preserve daemon code-first contract generation and supported shared contract sources; do not introduce an AO Cloud baseline or require private-submodule access.
- [ ] Document AO Cloud exclusions and validate only supported consumer behavior. Do not remove existing code or CI as part of this documentation task.

## Verification and delivery

Use one issue per PR targeting dev from an isolated task worktree. Cite exact scenario/test evidence and final commit; run complete applicable existing CI jobs for implementation changes. Documentation tasks validate source claims, links and examples. Respect the changed-Go-package coverage policy; native/device checks need actual platform evidence. Report unavailable checks explicitly. Never execute publication or production deployment as validation. Completion requires verified dev integration, not a draft PR or task checkbox alone.
