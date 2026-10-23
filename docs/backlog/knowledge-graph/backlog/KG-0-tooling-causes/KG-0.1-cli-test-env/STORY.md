# KG-0.1 · Story: CLI tests pass inside a worker session

**As** a worker running the backend suite inside its session **I want** the CLI
tests to ignore my session environment **so that** I do not chase spurious
failures (recorded lesson `c63c70c4`).

**Estimate:** 0.25 pd. **Dependencies:** none.

| Task | Name | Estimate |
|---|---|---:|
| KG-0.1.1 | Isolate CLI tests from the AO session environment | 0.25 pd |

**Acceptance criteria**

- [ ] The package suite passes with and without the session variables set.
