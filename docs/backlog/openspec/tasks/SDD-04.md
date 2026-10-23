# SDD-04 Baseline desktop and web behavior and accessibility

Issue: [#621](https://github.com/petarnenov/ultra-factory/issues/621).

## Outcome

Baseline desktop and web behavior and accessibility. Part of #617, the OpenSpec adoption tracker.

## Scope and integration points

openspec/specs/, frontend/src/{main,renderer,shared}/, frontend/e2e/, DESIGN.md.

Blueprint: https://github.com/petarnenov/ultra-factory/blob/docs/openspec-sdd-blueprint/docs/backlog/openspec/blueprint.md
Owner policy: [decisions D00–D13](https://github.com/petarnenov/ultra-factory/blob/docs/openspec-sdd-blueprint/docs/backlog/openspec/decisions.md). Read before implementation; all changes require a full proposal and geo.architect review.

Audit baseline: `060dd3cdb` (2026-10-23). Reconcile with current dev before implementation.

## Dependencies and priority

Hard dependencies: #619 (SDD-02), #620 (SDD-03).
Priority: `medium`. Normal migration work, scheduled after its foundations.
Responsible role: Capability owner; named assignee to be set when scheduled. Status: backlog.

## Acceptance criteria

- [ ] Cover macOS, Windows, Linux and Railway web under D04; map native Electron scenarios to actual-platform evidence, not renderer mocks alone.
- [ ] Specify IPC and thin-supervisor boundaries, session views, browser permissions, settings and web capability differences.
- [ ] Map loading/error/retry, keyboard/accessibility and host-scoped state scenarios to Vitest/Playwright coverage.
- [ ] Identify native Electron checks that renderer mocks cannot establish and define isolated desktop evidence procedures.
- [ ] Validate specs; record uncovered scenarios as owned backlog work without claiming new UI implementation.

## Verification and delivery

Use one issue per PR targeting dev from an isolated task worktree. Cite exact scenario/test evidence and final commit; run complete applicable existing CI jobs for implementation changes. Documentation tasks validate source claims, links and examples. Respect the changed-Go-package coverage policy; native/device checks need actual platform evidence. Report unavailable checks explicitly. Never execute publication or production deployment as validation. Completion requires verified dev integration, not a draft PR or task checkbox alone.
