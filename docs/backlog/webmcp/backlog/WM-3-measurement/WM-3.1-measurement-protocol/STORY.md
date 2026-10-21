# WM-3.1 · Story: Record format, protocol, and the measured data set

**As** a reader of the results **I want** a documented protocol and a
committed data set **so that** every number on the dashboard can be traced to
a run and reproduced.

**Estimate:** 1.5 pd. **Dependencies:** WM-2.2 for the runs.

| Task | Name | Estimate |
|---|---|---:|
| WM-3.1.1 | Record schema and measurement protocol | 0.5 pd |
| WM-3.1.2 | Run the benchmark and commit the data set | 1 pd |

**Acceptance criteria**

- [ ] The protocol names what is timed per driver and what is excluded
  (browser launch, dev server, setup, navigation, readiness, bridge steps).
- [ ] The data set validates against the schema and is imported by the
  dashboard without transformation.
- [ ] `summary.json` is produced by `frontend/scripts/benchmarks-summarize.mjs`
  and reproduces byte for byte.
