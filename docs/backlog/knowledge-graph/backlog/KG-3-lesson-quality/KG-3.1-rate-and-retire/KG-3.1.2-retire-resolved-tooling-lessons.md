# KG-3.1.2 · Task: Retire lessons already resolved in tooling

**Status:** ⬜ todo — [#561](https://github.com/petarnenov/ultra-factory/issues/561)

**Type:** chore (data). **Estimate:** 0.25 pd. **Story:** KG-3.1.
**Order:** 6 of 8. **Depends on:** KG-3.1.1 (#560) rolled out; KG-0.1.1 (#550) merged.
**Next:** KG-4.1.1 (#564).

## Goal

Retire recorded lessons whose cause is already fixed, with the fix as the
reason.

## Context (verified 2026-10-23)

| Lesson id | Text starts with | Resolved by |
|---|---|---|
| `638bc529-1b90-46b8-a920-24d04c80c44d` | "make ci-check ARGS='--only comment,lint' needs 'npm ci …'" | `scripts/ci-check.mjs` lines 257–263 print the fix |
| `5fb8d34c-0486-4c0c-97b8-fdfa2b839134` | "Session branch ao/<id> blocks slash children …" | `session_manager/prompt.go` `workerMultiPRPrompt` hyphen-sibling rule |
| `c63c70c4-deb2-43e4-b1bc-c49c7542f646` | "Issue acceptance gaps: check the issue comments first; ao CLI tests need …" | Only partly: KG-0.1.1 fixes the CLI part; the issue-comments advice stays useful |

## Subtasks

- [ ] Re-read each lesson in full and confirm the fix on current `dev`.
- [ ] Retire `638bc529` and `5fb8d34c` with `ao knowledge retire`.
- [ ] For `c63c70c4`: retire it and record the remaining advice as a new
  `tip` without the CLI part, or keep it; note the choice in the issue.
- [ ] Shared-graph writes need the user's approval.

## Acceptance criteria

- [ ] The retired lessons are absent from `ao knowledge search` results.

## Verification

`ao knowledge search --project ultra-factory "ci-check npm"` and
`"branch slash children"` return no retired lesson.
