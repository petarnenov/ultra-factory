# Proposal

## Why

`openspec/specs/` currently defines only the OpenSpec setup check; it does not capture core daemon, CLI, lifecycle, Chat/TUI recovery, or SQLite contracts. Issue #620 baselines these behaviors against current code, tests, and architecture while keeping known defects explicit.

Approved proposal: revision 5, SHA-256 `633bc0d42178da89952f9bd3a23590d797208e7c7d43d2609a53f67c9acaacd5`, base `267d51eb1047a84d8403cb37e16dd1d8fb3e0dd8`. D03 approval is the exact single-arbiter Codex Astra ruling archived in the task evidence; it is not council consensus. Revisions 1–4, including their rejections and the Fable quota failure, remain in the same consultation archive.

## What Changes

- Add six core capability specifications with stable R620/S620 identifiers and concrete existing test mappings.
- Record the D06 workspace ruling: ordinary cleanup refuses and preserves dirty registered worktrees; direct uncaptured `ForceDestroy` is a defect, and no preserve-before-force exception is approved.
- Distinguish immutable shipped migration files from narrowly identified legacy Goose ledger repairs; do not claim concurrent-open safety without evidence.
- Keep the ForceDestroy runtime follow-up in issue #659, outside this documentation-only change.
- Track SQLite repair concurrency/history evidence in #661, API redaction/request-ID assertions in #662, and the exact bounded `session_manager` test failure in #663; no backend source/test fixes are part of #620.
- Add proposal, design, tasks, and verification evidence for this baseline in the same OpenSpec change and archive it after the required review.
- Make no backend, CLI, API, migration, query, CI, or unrelated capability changes.

## Capabilities

### New Capabilities

- `session-lifecycle`: Derived session display status and uncertain runtime observations.
- `workspace-lifecycle`: Workspace setup/recovery and dirty-worktree preservation during ordinary cleanup.
- `chat-tui-recovery`: Durable Chat recovery and safe TUI/Chat controller fencing.
- `daemon-api-errors`: Stable daemon error envelope and request correlation.
- `cli-error-contract`: CLI usage/runtime exit codes and daemon error preservation.
- `sqlite-upgrades-cdc`: Append-only SQLite upgrades and trigger-backed change events.

### Modified Capabilities

None. The existing `openspec-check-setup` capability is unchanged.

## Impact

Documentation-only changes under `openspec/specs/` and this OpenSpec change/archive. Evidence references read-only Go sources and tests. API authority remains `backend/internal/httpd/controllers/dto.go` and `backend/internal/httpd/apispec/specgen/build.go`; SQLite evidence includes migrations, queries, `db.go` compatibility repairs, generated sqlc output, stores, and CDC. No runtime behavior changes.
