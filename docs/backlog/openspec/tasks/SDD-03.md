# SDD-03 Baseline daemon, CLI, lifecycle and storage capabilities

Issue: [#620](https://github.com/petarnenov/ultra-factory/issues/620).

## Outcome

Baseline daemon, CLI, lifecycle and storage capabilities. Part of #617, the OpenSpec adoption tracker.

## Scope and integration points

openspec/specs/, backend/internal/{domain,ports,service,cli,httpd,lifecycle,storage,cdc,observe}/.

Blueprint: https://github.com/petarnenov/ultra-factory/blob/docs/openspec-sdd-blueprint/docs/backlog/openspec/blueprint.md
Audit baseline: `060dd3cdb` (2026-10-23). Reconcile with current dev before implementation.

## Dependencies and priority

Hard dependencies: #618 (SDD-01), #619 (SDD-02), #633 (SDD-15).
Priority: `high`. Blocks the foundation or verification gates of this adoption batch.
Responsible role: Backend owner; named assignee to be set when scheduled. Status: backlog.

## Acceptance criteria

- [ ] Resolve code/test/documentation contradictions via geo.architect under D06/D03, record evidence and decisions, and keep unresolved defects visible rather than blessing current behavior automatically.
- [ ] Map session/workspace lifecycle, Chat/TUI recovery, API/CLI error contracts, SQLite upgrades and CDC to evidence-backed scenarios.
- [ ] Preserve derived status, unknown-probe handling, dirty-worktree safety and append-only migrations.
- [ ] Reference DTO/registry and sqlc sources; map concrete existing test names, recording missing behavior as gap issues.
- [ ] Validate seeded specs and review representative negative/restart/concurrency scenarios with backend owners.

## Verification and delivery

Use one issue per PR targeting dev from an isolated task worktree. Cite exact scenario/test evidence and final commit; run complete applicable existing CI jobs for implementation changes. Documentation tasks validate source claims, links and examples. Respect the changed-Go-package coverage policy; native/device checks need actual platform evidence. Report unavailable checks explicitly. Never execute publication or production deployment as validation. Completion requires verified dev integration, not a draft PR or task checkbox alone.
