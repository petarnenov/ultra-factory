# KG-0 · Epic: Fix the tooling cause behind a recorded lesson

**Status:** ⬜ todo — [#548](https://github.com/petarnenov/ultra-factory/issues/548)

**Goal.** A lesson that only exists because a tool is broken should disappear
once the tool is fixed. Of the three tooling lessons recorded so far, two are
already resolved in code (the `ci-check` fix hint, the branch guidance in the
worker prompt); one is still broken.

**Estimate:** 0.25 pd. **Stage:** first. **Dependencies:** none.

| Story | Name | Estimate |
|---|---|---:|
| KG-0.1 | CLI tests pass inside a worker session | 0.25 pd |

**Acceptance criteria**

- [ ] `go test ./internal/cli/` passes with `AO_SESSION_ID` and `AO_PROJECT_ID` set.
