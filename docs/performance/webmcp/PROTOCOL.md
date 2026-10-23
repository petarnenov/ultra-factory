# WebMCP UI benchmark protocol

This protocol defines how the WebMCP UI benchmark is measured, recorded, and
summarized. It covers three drivers (`playwright`, `webmcp-scripted`,
`webmcp-jev`) over the scenario catalog plus the overhead series. The decisions
behind it are in `docs/backlog/webmcp/webmcp-report.md` (section 1a). The
record type and validator are in
`frontend/src/renderer/benchmarks/record.ts`, shared by the e2e bench spec and
the dashboard.

Protocol version: `1`.

The report and dashboard show only numbers from `summary.json`. Per-attempt
records are raw evidence; they are never rendered directly.

## 1. Record

Each measured or warm-up attempt is one `BenchRecord` (`schema: 1`) with
`protocolVersion: 1`. Fields:

- `runId`: unique batch identity, generated once per run of the spec. A rerun
  gets a new ID, so records from different runs never merge.
- `protocolVersion`: protocol version used for the measurement.
- `label`, `commit`, `timestamp`: from `AO_PERF_LABEL`, `git rev-parse HEAD`,
  and the attempt's start time.
- `env`: `chromium` from `browser.version()` (always present); `electron` only
  when the run uses `test/e2e-pod/playwright.electron.config.ts`, otherwise
  omitted; `node`, `platform`, `arch`, `cpus`, `cpuModel`, `totalMemoryBytes`,
  and `viewport` describe the machine and page.
- `scenario`, `page` (`board`, `settings`, `chat`, `overhead`), `driver`,
  `repeat`, `warmup`.
- `pass`, `error`: a failed attempt carries `pass: false` and the error text.
- `totalMs`, `assertMs`: see section 2.
- `steps`: one entry per tool call or decision. Empty for `playwright`.
- `jev`: present for `webmcp-jev` only (section 5).

`validateRecord` throws an error listing every missing or mistyped field. Any
record that fails validation is rejected, not repaired.

## 2. Timed window per driver

The timed window is the same for every driver:

- **Starts** after `setup`, `goto`, and readiness. Readiness means the WebMCP
  tools are listed, or for Playwright the first `expect(...).toBeVisible()` of
  the original spec.
- **Ends** after the last assertion of the scenario.
- **Excludes** browser launch, the dev server, `setup`, `goto`, the readiness
  wait, and `bridge` steps. A bridge step is an untimed test-bridge push (for
  example a backend state change) that runs once at its phase boundary. Its
  duration is accumulated and subtracted from `totalMs`.

`totalMs` is the timed window minus bridge time. `assertMs` is the measured
assertion time, included in `totalMs`. It is measured explicitly around the
assertions, not derived as `totalMs - decisionMs - execMs`: that residual also
contains browser transport and driver overhead.

Per-step times in `steps` (`execMs`, `roundTripMs`, `decisionMs`) are shown as
component breakdowns only. Their medians need not sum to the median of
`totalMs`. The dashboard and report label this limitation.

## 3. Repeats and machine conditions

- Repeats: `AO_WEBMCP_REPEATS=6`, which is one warm-up plus five measured
  repeats. The warm-up is written with `warmup: true` and dropped by the
  summarizer.
- Attempts run serially with `--workers=1 --retries=0`. Each
  driver/scenario attempt is an independent test. A failed attempt does not
  skip the remaining scenarios or repeats.
- `CI=true` is set, so the dev server is not reused between attempts.
- One machine per batch. Nothing else runs during the batch (no other builds,
  tests, or sessions). The laptop stays on mains power.
- Overhead series: `overhead.evaluate-noop` calls a `noop` tool 50 times in
  one timed window, recorded like any scenario.

Scenario count: ten catalog scenarios plus the overhead series. A full batch is
therefore `(10 + 1) × 3 drivers × 6 attempts` = 198 records when a JEV key is
present, and `(10 + 1) × 2 × 6` = 132 without one.

## 4. Failure handling

- A failed attempt is recorded with `pass: false`, the elapsed time, the
  partial `steps`, and `error`. It is written before the test fails.
- Setup failures (before the timed window starts) are reported separately and
  do not produce a measurement record.
- Pass rate counts failed measured attempts. Latency statistics do not.
- A HTTP attempt status is `null` when there was no response. Statuses are
  never invented.

## 5. JEV driver

- Model: `@typesafe-ai/sdk` 0.7.0 with `maxRetries: 0`. The driver makes one
  manual retry only, on HTTP 429 or 5xx, and records both attempts in
  `jev.attempts` (`status` and `ms`); a missing response is `status: null`.
  Retry waiting is counted in the decision time, not in `execMs`.
- `jev.model`, `inputTokens`, and `outputTokens` are the sums over all decisions
  in the attempt, including the terminal `done` decision. The `done` decision
  is recorded as a step, so its `decisionMs` and tokens are counted.
- Step cap: 3× the number of scripted steps for the scenario. A read-only
  scenario gets a terminal decision allowance, so its cap is never zero.
- Prompt text and criteria strings are fixed in code, so token counts are
  comparable across repeats.
- The API key is read from `TYPESAFE_API_KEY` (or `JEV_MAF_LAB`), passed as
  `apiKey`, and never logged or printed. Do not paste a key into commands,
  records, reports, or issues.

## 6. Statistics

Computed by `summarize` in `record.ts`. It is pure: the same records in any
order give the same rows, and rows are sorted by scenario, page, then driver.

- Warm-up records are dropped.
- `n`: measured attempts, including failed ones. `passRate`: passing attempts
  divided by `n`.
- `medianMs`, `minMs`, `maxMs`: over passing measured attempts' `totalMs`.
  These are the median and min–max over measured repeats.
- `ratioToPlaywright` = driver `medianMs` ÷ Playwright `medianMs` for the same
  scenario and page. The Playwright row's ratio is therefore 1 when it has a
  median.
- When no measured attempt passes, `medianMs`, `minMs`, `maxMs`, and the ratio
  are `null`. A ratio is also `null` when the Playwright baseline is missing or
  zero. Never `0` or `NaN`.
- `jev` (webmcp-jev rows only), over passing attempts: `medianDecisionMs` (sum
  of `decisionMs` per attempt), `medianExecMs` (sum of `execMs` per attempt),
  `medianAssertMs`, `medianSteps`, and `matchRate` (matched steps ÷ steps that
  carry `matchedScripted`). All are `null` when no attempt passed.

Overhead reference: the overhead row's `medianMs` covers 50 `noop` calls, so
the median per call is `medianMs ÷ 50`. Show it as a reference line. It is
never subtracted from other scenarios.

## 7. Reproduce

Run from the repository root. Substitute a real key for `<key>` in your own
shell; do not commit it. Batch outputs go outside the repository.

Scripted WebMCP driver and Playwright baseline (WM-2.2.1):

```bash
AO_WEBMCP_BENCH=1 AO_PERF_DIR=/tmp/wm AO_PERF_LABEL=dev AO_E2E_PORT=5292 CI=true npm --prefix frontend run test:e2e -- e2e/webmcp-bench.spec.ts --workers=1 --retries=0
```

Without a JEV key this writes `(10 + 1) × 2 × 6` files to `/tmp/wm`.

JEV driver (WM-2.2.2), opt-in only and never part of CI:

```bash
AO_WEBMCP_BENCH=1 TYPESAFE_API_KEY=<key> AO_PERF_DIR=/tmp/wm AO_E2E_PORT=5292 CI=true npm --prefix frontend run test:e2e -- e2e/webmcp-bench.spec.ts --workers=1 --grep webmcp-jev
```

Set `AO_WEBMCP_REPEATS` only to change the repeat count from the default of 6.
Keep the machine state from section 3 for both runs.

Validation and summary:

```bash
npm --prefix frontend test -- src/renderer/benchmarks
npm --prefix frontend run typecheck && npm --prefix frontend run typecheck:e2e
```

Turning `/tmp/wm` into `summary.json` is the job of WM-3.1.2's summarizer,
which reads each per-attempt file, runs `validateRecord`, and calls
`summarize`. Until that exists, the per-attempt files are the only output.
