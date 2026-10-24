# Proposal

## Why

`openspec/specs/` currently defines only the OpenSpec setup check; it does not capture core daemon, CLI, lifecycle, Chat/TUI recovery, or SQLite contracts. Issue #620 baselines these behaviors against current code, tests, and architecture while keeping known defects explicit.

Approved proposal: revision 3, SHA-256 `138e961c4ff2314322a29bb1464e1e335701114cc91450b8bc291b590bee2eb0`, base `267d51eb1047a84d8403cb37e16dd1d8fb3e0dd8`. D03 approval is the exact single-arbiter Codex Astra ruling archived in the task evidence; it is not council consensus.

## What Changes

- Add six core capability specifications with stable R620/S620 identifiers and concrete existing test mappings.
- Record the D06 workspace ruling: ordinary cleanup refuses and preserves dirty registered worktrees; direct uncaptured `ForceDestroy` is a defect, and no preserve-before-force exception is approved.
- Keep the ForceDestroy runtime follow-up in issue #659, outside this documentation-only change.
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
