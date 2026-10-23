# KG-0.1.1 · Task: Isolate CLI tests from the AO session environment

**Status:** ⬜ todo — [#550](https://github.com/petarnenov/ultra-factory/issues/550)

**Type:** bug (tests). **Estimate:** 0.25 pd. **Story:** KG-0.1.
**Order:** 1 of 8. **Depends on:** none. **Next:** KG-1.1.1 (#553).

## Goal

`go test ./internal/cli/` passes when it runs inside an AO worker, where
`AO_SESSION_ID` and `AO_PROJECT_ID` are set.

## Context (verified 2026-10-23)

- `AO_SESSION_ID=ultra-factory-999 AO_PROJECT_ID=ultra-factory go test ./internal/cli/`
  fails `TestSpawnCommand_MissingProjectContext` (`internal/cli/spawn_test.go:59`)
  and `TestSpawnResolvesProjectFromCWD` (`internal/cli/spawn_test.go:459`).
- Other tests already clear the variable one by one, e.g.
  `internal/cli/cue_test.go:15`, `internal/cli/ownership_test.go:120`.
- The package `TestMain` is in `internal/cli/e2e_test.go:35`.

## Subtasks

- [ ] Clear the session variables for the whole package: unset them in
  `TestMain` before `m.Run()`, or call `t.Setenv(..., "")` in the two
  failing tests. Prefer the `TestMain` route if no test relies on an
  inherited value.
- [ ] Keep the existing per-test `t.Setenv` calls that set explicit values.

## Acceptance criteria

- [ ] The command above passes.
- [ ] `go test ./internal/cli/` passes without the variables.

## Verification

Both commands above, plus `make ci-check` before the PR.
