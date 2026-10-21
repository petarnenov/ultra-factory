# WM-4 · Epic: Benchmarks dashboard page and report

**Status:** ⬜ todo — [#486](https://github.com/petarnenov/ultra-factory/issues/486)

**Goal.** Add a dashboard page that compares the three drivers per scenario
with charts, fed by the committed data set, and publish the written report in
the format of `docs/performance/chat-responsiveness/`.

**Estimate:** 2–2.5 pd. **Stage:** publish. **Dependencies:** WM-3.1.1
(types) for WM-4.1.1 and WM-4.1.2; WM-3.1.2 (data) for the final numbers.

| Story | Name | Estimate |
|---|---|---:|
| WM-4.1 | Benchmarks page with charts and the report | 2–2.5 pd |

**Decisions (resolved 2026-10-21)**

- Charts: `recharts` through a shadcn `components/ui/chart.tsx` primitive,
  using the existing `--chart-1..5` tokens.
- Route `/#/benchmarks`, sidebar row after Artifacts, command palette entry.

**Acceptance criteria**

- [ ] `/#/benchmarks` renders a summary table and four charts (totals with
  min–max, JEV decision/exec/assert split, repeat distribution with failures
  marked, pass rate) from the committed data set; follows `DESIGN.md`; built
  from `components/ui/*`.
- [ ] Every chart has a table twin with the same numbers.
- [ ] Reachable from the sidebar and the palette; i18n keys in all locales.
- [ ] `docs/performance/webmcp/README.md` states method, results, reproduce
  commands, verification, screenshots, and deliberate limits.

**Out of scope.** Live data from the daemon; triggering runs from the UI;
historical trend across commits.
