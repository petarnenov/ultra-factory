# SDD-05 Baseline mobile contracts and device verification

Issue: [#622](https://github.com/petarnenov/ultra-factory/issues/622).

## Outcome

Baseline mobile contracts and device verification. Part of #617, the OpenSpec adoption tracker.

## Scope and integration points

openspec/specs/, packages/mobile/, .github/workflows/mobile.yml.

Blueprint: https://github.com/petarnenov/ultra-factory/blob/docs/openspec-sdd-blueprint/docs/backlog/openspec/blueprint.md
Audit baseline: `060dd3cdb` (2026-10-23). Reconcile with current dev before implementation.

## Dependencies and priority

Hard dependencies: #619 (SDD-02), #620 (SDD-03).
Priority: `medium`. Normal migration work, scheduled after its foundations.
Responsible role: Capability owner; named assignee to be set when scheduled. Status: backlog.

## Acceptance criteria

- [ ] Capture pairing, identity/version negotiation, authenticated transport, reconnect, push and Chat/TUI compatibility.
- [ ] Map current typecheck/unit checks and distinguish them from iOS/Android device acceptance.
- [ ] Define negative pairing, expired authorization, offline recovery and unsupported capability scenarios with evidence owners.
- [ ] Validate specs and record device/platform gaps explicitly.

## Verification and delivery

Use one issue per PR targeting dev from an isolated task worktree. Cite exact scenario/test evidence and final commit; run complete applicable existing CI jobs for implementation changes. Documentation tasks validate source claims, links and examples. Respect the changed-Go-package coverage policy; native/device checks need actual platform evidence. Report unavailable checks explicitly. Never execute publication or production deployment as validation. Completion requires verified dev integration, not a draft PR or task checkbox alone.
