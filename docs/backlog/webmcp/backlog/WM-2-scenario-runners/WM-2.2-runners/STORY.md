# WM-2.2 · Story: Playwright, scripted, and JEV drivers in one bench spec

**As** a benchmark author **I want** three drivers that execute the catalog
under one clock, one copying the original Playwright steps, one calling tools
in order, and one where JEV picks each tool, **so that** the comparison is
fair and repeatable.

**Estimate:** 2–2.5 pd. **Dependencies:** WM-2.1.1.

| Task | Name | Estimate |
|---|---|---:|
| WM-2.2.1 | Scripted WebMCP driver, Playwright driver, and bench spec | 1 pd |
| WM-2.2.2 | JEV driver | 1–1.5 pd |

**Acceptance criteria**

- [ ] All drivers run inside `frontend/e2e/webmcp-bench.spec.ts` and use the
  shared assertion layer.
- [ ] Tool drivers return per-step timings; Playwright returns aggregate timing
  with an empty tool-step list. All drivers return pass/fail that the bench spec
  records in the WM-3.1.1 format.
- [ ] The JEV driver uses `@typesafe-ai/sdk` with retries disabled, reads the
  key from `TYPESAFE_API_KEY` or `JEV_MAF_LAB`, and skips with a clear reason
  when neither is set.
