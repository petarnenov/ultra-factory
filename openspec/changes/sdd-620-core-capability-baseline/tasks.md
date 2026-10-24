# Tasks

## 1. Seed approved capability deltas

- [ ] 1.1 Add session and workspace lifecycle specs with R620-SL/R620-WL and S620-SL/S620-WL identifiers; verify strict OpenSpec validation and the exact source/test entries in `verification.md`.
- [ ] 1.2 Add Chat/TUI recovery and daemon API error specs with R620-CR/R620-API and S620-CR/S620-API identifiers; verify strict OpenSpec validation and negative, restart, and fencing evidence mappings.
- [ ] 1.3 Add CLI error and SQLite upgrade/CDC specs with R620-CLI/R620-DB and S620-CLI/S620-DB identifiers; verify strict OpenSpec validation, exact test parents/subtests, and the `e2e` tag boundary.

## 2. Reconcile evidence and validate

- [ ] 2.1 Complete `verification.md` with source paths and exact test names for every scenario; verify each selector against the frozen base and record actual execution status.
- [ ] 2.2 Obtain backend-owner semantic review of representative negative, restart, and concurrency/fencing scenarios and the D06 workspace disposition; record any unresolved behavior only as an owned gap.
- [ ] 2.3 Run pinned OpenSpec strict validation and applicable documentation/policy checks; verify all six deltas are complete, source claims are accurate, and `git diff --check` passes.

## Workflow follow-up

- Archive this change after the tracked work is complete; include the archived change and decision/evidence record in the #620 delivery PR.
- Exact-head static review, applicable remote CI, manual completeness disposition, and integration are delivery steps recorded on the PR, not tracked tasks inside the archived change.
- The PR targets `dev` and closes #620 only; #659 remains a separate backend follow-up.
