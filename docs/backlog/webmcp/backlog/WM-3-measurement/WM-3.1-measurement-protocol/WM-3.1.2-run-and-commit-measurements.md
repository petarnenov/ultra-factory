# WM-3.1.2 · Task: Run the benchmark and commit the data set

**Status:** ⬜ todo — [#485](https://github.com/petarnenov/ultra-factory/issues/485)

**Type:** operations and measurement. **Estimate:** 1 pd. **Story:** WM-3.1.

## Goal

Run the bench spec per `PROTOCOL.md` on the machine that holds the JEV key,
and commit the raw records and summary as the dashboard's data source.

## Decisions (resolved)

- Data source for the dashboard is **committed JSON in the renderer**:
  `frontend/src/renderer/benchmarks/data/measurements.json` (array of
  `BenchRecord`) and `summary.json` (array of `BenchSummaryRow`). No daemon
  API.
- A raw copy is kept at `docs/performance/webmcp/measurements.json` for the
  report; the summarizer verifies both copies are identical.

## Context

- Machine: the registered host whose daemon exposes `JEV_MAF_LAB` (or
  `TYPESAFE_API_KEY`) to its sessions; the orchestrator finds it with a probe
  worker and never names it in advance (the 2026-10-21 probe measured
  151–242 ms per JEV call from that host).
- `AO_PERF_DIR` output is one file per run; the summarizer concatenates.

## Blueprint

```js
// frontend/scripts/benchmarks-summarize.mjs
// usage: node frontend/scripts/benchmarks-summarize.mjs <dir-or-measurements.json> [--out frontend/src/renderer/benchmarks/data]
// 1. read all *.json (or the array file); 2. validateRecord each; 3. write measurements.json (sorted by scenario, driver, repeat);
// 4. write summary.json = summarize(records); 5. print a Markdown table (scenario × driver → median (min–max), pass rate) for the report.
```

Run sequence:

```bash
export AO_WEBMCP_BENCH=1 AO_WEBMCP_REPEATS=6 AO_PERF_LABEL=wm-2026-10 AO_PERF_DIR=/tmp/wm AO_E2E_PORT=5292 CI=true
export TYPESAFE_API_KEY="$JEV_MAF_LAB"
npm --prefix frontend run test:e2e -- e2e/webmcp-bench.spec.ts --workers=1
node frontend/scripts/benchmarks-summarize.mjs /tmp/wm --out frontend/src/renderer/benchmarks/data
cp frontend/src/renderer/benchmarks/data/measurements.json docs/performance/webmcp/measurements.json
```

## Subtasks

- [ ] `benchmarks-summarize.mjs` (imports `summarize`/`validateRecord` from
  the built `record.ts` via `tsx` or a small esbuild step; decide in the PR
  and note it).
- [ ] Execute the run sequence; if any JEV repeat fails, keep it and re-run
  that scenario once more (records are appended, `n` grows).
- [ ] Commit `data/measurements.json`, `data/summary.json`,
  `docs/performance/webmcp/measurements.json`.
- [ ] Paste the printed Markdown table into the PR description.

## Acceptance criteria

- [ ] ≥ 5 measured repeats per (scenario, driver) for `playwright` and
  `webmcp-scripted`; ≥ 5 attempts for `webmcp-jev` with failures kept.
- [ ] `summary.json` reproduces byte for byte from `measurements.json`.
- [ ] `git grep -I "$JEV_MAF_LAB"` finds nothing; records contain no key.

## Verification

- `node frontend/scripts/benchmarks-summarize.mjs docs/performance/webmcp/measurements.json --check` exits 0.

## Dependencies

WM-3.1.1, WM-2.2.2.
