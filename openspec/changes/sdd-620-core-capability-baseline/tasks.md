# Tasks

## 1. Seed approved capability deltas

- [x] 1.1 Add session and workspace lifecycle specs with R620-SL/R620-WL and S620-SL/S620-WL identifiers; verify strict OpenSpec validation and the exact source/test entries in `verification.md`.
- [x] 1.2 Add Chat/TUI recovery and daemon API error specs with R620-CR/R620-API and S620-CR/S620-API identifiers; verify strict OpenSpec validation and negative, restart, and fencing evidence mappings.
- [x] 1.3 Add CLI error and SQLite upgrade/CDC specs with R620-CLI/R620-DB and S620-CLI/S620-DB identifiers; verify strict OpenSpec validation, exact test parents/subtests, and both `e2e` and `chatui_regression` tag boundaries.

## 2. Reconcile evidence and validate

- [x] 2.1 Complete `verification.md` with source paths and exact test names for every scenario; verify each selector against the frozen base and record actual execution status.
- [ ] 2.2 Obtain backend-owner semantic review of representative negative, restart, and concurrency/fencing scenarios and the D06 workspace disposition; record any unresolved behavior only as an owned gap.
- [x] 2.3 Reconcile the bounded `session_manager` package failure using the focused diagnostic tracked in [#663](https://github.com/petarnenov/ultra-factory/issues/663); record the exact test and error or the unrecoverable limitation.
- [ ] 2.4 Validate the SQLite ledger-repair boundary against owner-backed evidence and [#661](https://github.com/petarnenov/ultra-factory/issues/661); no simultaneous-open safety claim is accepted without evidence.
- [x] 2.5 Validate API redaction and request correlation claims against direct assertions or explicitly bounded gap [#662](https://github.com/petarnenov/ultra-factory/issues/662).
- [x] 2.6 Run pinned OpenSpec strict validation and applicable documentation checks; verify all six deltas are complete, source claims are accurate, and `git diff --check` passes. Policy checks remain on the final-head delivery gate.

## Workflow follow-up

- Archive this change only after all remaining acceptance tasks and final semantic disposition pass; include the archived change and complete decision/evidence record in the #620 delivery PR.
- Exact-head static review, applicable remote CI, manual completeness disposition, and integration are delivery steps recorded on the PR, not tracked tasks inside the archived change.
- The PR targets `dev` and closes #620 only; #659, #661, #662, and #663 remain separate backend follow-ups.
