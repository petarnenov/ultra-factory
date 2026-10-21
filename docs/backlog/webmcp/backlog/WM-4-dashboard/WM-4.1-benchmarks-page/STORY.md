# WM-4.1 · Story: Benchmarks page with charts and the report

**As** a maintainer **I want** a page in the app that shows Playwright vs
WebMCP scripted vs WebMCP + JEV per scenario, with charts, **so that** the
trade-off is visible without opening JSON files.

**Estimate:** 2–2.5 pd. **Dependencies:** WM-3.1.1 (types), WM-3.1.2 (data).

| Task | Name | Estimate |
|---|---|---:|
| WM-4.1.1 | Chart primitive (`components/ui/chart.tsx`) | 0.5 pd |
| WM-4.1.2 | `/#/benchmarks` route, data loader, comparison views | 1–1.5 pd |
| WM-4.1.3 | Performance report under `docs/performance/webmcp/` | 0.5 pd |

**Acceptance criteria**

- [ ] The page works in light and dark themes and at 390 px width.
- [ ] Every chart has a table equivalent for the same numbers.
- [ ] The report links the data set and the page; measured results trace to
  `summary.json` or raw records, and environment metadata to `env`.
