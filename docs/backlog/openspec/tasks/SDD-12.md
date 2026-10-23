# SDD-12 Connect operations, incidents and deprecation to specifications

Issue: [#629](https://github.com/petarnenov/ultra-factory/issues/629).

## Outcome

Connect operations, incidents and deprecation to specifications. Part of #617, the OpenSpec adoption tracker.

## Scope and integration points

docs/runbooks/, deploy/railway/README.md, docs/backlog/knowledge-graph/.

Blueprint: https://github.com/petarnenov/ultra-factory/blob/docs/openspec-sdd-blueprint/docs/backlog/openspec/blueprint.md
Audit baseline: `060dd3cdb` (2026-10-23). Reconcile with current dev before implementation.

## Dependencies and priority

Hard dependencies: #623 (SDD-06), #624 (SDD-07), #626 (SDD-09).
Priority: `medium`. Normal migration work, scheduled after its foundations.
Responsible role: Capability owner; named assignee to be set when scheduled. Status: backlog.

## Acceptance criteria

- [ ] Apply D11: agent automation ends at dev. Promotion PR creation, main merges, publishing and deployment are owner-operated; this task supplies documentation and evidence without starting those operations.
- [ ] Apply D09 when hardware/access/evidence is unavailable: record the exact affected acceptance and owned follow-up, continue independent approved work, and distinguish verified delivery from outstanding checks. Do not claim full verification from missing evidence.
- [ ] Define evidence-backed backup/restore, drain, recovery, observability and rollback acceptance with environment/commit/date.
- [ ] Convert a representative incident into a regression scenario, test link and follow-up issue.
- [ ] Define removal/deprecation deltas, migration/retention impact and runbook/manual updates.
- [ ] Keep knowledge graph lessons advisory; record controlled rehearsal results or explicit unrun procedures without claiming production verification.

## Verification and delivery

Use one issue per PR targeting dev from an isolated task worktree. Cite exact scenario/test evidence and final commit; run complete applicable existing CI jobs for implementation changes. Documentation tasks validate source claims, links and examples. Respect the changed-Go-package coverage policy; native/device checks need actual platform evidence. Report unavailable checks explicitly. Never execute publication or production deployment as validation. Completion requires verified dev integration, not a draft PR or task checkbox alone.
