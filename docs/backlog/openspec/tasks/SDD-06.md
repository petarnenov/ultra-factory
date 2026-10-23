# SDD-06 Baseline public cloud, shared contracts and Railway

Issue: [#623](https://github.com/petarnenov/ultra-factory/issues/623).

## Outcome

Baseline public cloud, shared contracts and Railway. Part of #617, the OpenSpec adoption tracker.

## Scope and integration points

openspec/specs/, cloud/, contracts/cloud/, packages/{cloud-client,product-ui,hub-bff}/, deploy/railway/.

Blueprint: https://github.com/petarnenov/ultra-factory/blob/docs/openspec-sdd-blueprint/docs/backlog/openspec/blueprint.md
Audit baseline: `060dd3cdb` (2026-10-23). Reconcile with current dev before implementation.

## Dependencies and priority

Hard dependencies: #619 (SDD-02), #620 (SDD-03).
Priority: `medium`. Normal migration work, scheduled after its foundations.
Responsible role: Capability owner; named assignee to be set when scheduled. Status: backlog.

## Acceptance criteria

- [ ] Map tenant/auth/billing, worker execution/reconciliation and PostgreSQL upgrades to public code/test evidence.
- [ ] Document cloud contract generation and shared consumer compatibility independently from daemon code-first generation.
- [ ] Cover BFF cookie/CSRF, REST/SSE/mux proxying, remote workers, drain and backup/restore scenarios.
- [ ] Record optional private submodule ownership and obtain owner evidence for private integration rather than claiming public inspection proves it.

## Verification and delivery

Use one issue per PR targeting dev from an isolated task worktree. Cite exact scenario/test evidence and final commit; run complete applicable existing CI jobs for implementation changes. Documentation tasks validate source claims, links and examples. Respect the changed-Go-package coverage policy; native/device checks need actual platform evidence. Report unavailable checks explicitly. Never execute publication or production deployment as validation. Completion requires verified dev integration, not a draft PR or task checkbox alone.
