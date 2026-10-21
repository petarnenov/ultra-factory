# WM-3 · Epic: Measurement protocol and measured runs

**Status:** ⬜ todo — [#482](https://github.com/petarnenov/ultra-factory/issues/482)

**Goal.** Fix the record format and the protocol so the three drivers are
compared on equal terms, then produce the committed data set the dashboard
and the report are built from.

**Estimate:** 1.5 pd. **Stage:** measure. **Dependencies:** WM-2 (runs);
WM-3.1.1 has no dependency and can start first.

| Story | Name | Estimate |
|---|---|---:|
| WM-3.1 | Record format, protocol, and the measured data set | 1.5 pd |

**Decisions (resolved 2026-10-21)**

- Data lives as committed JSON in
  `frontend/src/renderer/benchmarks/data/` (plus a raw copy under
  `docs/performance/webmcp/`); no daemon API.

**Acceptance criteria**

- [ ] One record type (`benchmarks/record.ts`) with `validateRecord` and a
  pure `summarize`, shared by the bench spec and the dashboard.
- [ ] A committed data set with ≥ 5 measured repeats per (scenario, driver)
  on one machine, including the `overhead.evaluate-noop` series.
- [ ] Records carry commit, Chromium (and Electron when applicable), Node,
  platform, hardware, and the JEV model version returned by the API.
- [ ] Pass rate per driver is reported next to timing; failed repeats are
  kept and flagged, not dropped.
