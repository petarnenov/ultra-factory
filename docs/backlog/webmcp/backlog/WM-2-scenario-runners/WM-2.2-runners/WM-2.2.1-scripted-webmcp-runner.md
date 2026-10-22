# WM-2.2.1 · Task: Scripted WebMCP driver, Playwright driver, and bench spec

**Status:** ⬜ todo — [#480](https://github.com/petarnenov/ultra-factory/issues/480)

**Type:** tests. **Estimate:** 1 pd. **Story:** WM-2.2.

## Goal

Add `frontend/e2e/webmcp/drivers/{scripted,playwright}.ts` and
`frontend/e2e/webmcp-bench.spec.ts` that runs every catalog scenario through
each driver with one clock and writes one record per run.

## Context (verified)

- Pattern: `frontend/e2e/chat-performance.spec.ts` — gated by
  `AO_PERF_BENCH=1` (line 16), record fields at lines 41-58 (`commit` via
  `git rev-parse HEAD`, `browser.version()`, `process.version`,
  `os.platform()/arch()`, `os.cpus()`, `os.totalmem()`, viewport, timestamp),
  file write at lines 63-77 to `AO_PERF_DIR/${label}-${workload}-${repeat}.json`.
- Playwright config `frontend/playwright.config.ts`: `testDir: "e2e"`,
  `baseURL` from `AO_E2E_PORT`, `webServer` = `npm run dev:web`.

## Blueprint

The snippets show the successful path. Implement the failure capture described
below inside each driver; the spec must receive a complete failed record.

```ts
// frontend/e2e/webmcp/drivers/types.ts
export type StepTiming = { index: number; tool: string; execMs: number; roundTripMs: number;
  decisionMs?: number; choice?: string; confidence?: number; matchedScripted?: boolean };
export type DriverResult = { pass: boolean; totalMs: number; assertMs: number; steps: StepTiming[]; error?: string };
export type Driver = (page: Page, host: HostState, s: Scenario) => Promise<DriverResult>;

// drivers/scripted.ts
export const scripted: Driver = async (page, host, s) => {
  await waitForTools(page, `${s.page}.`);
  let t0 = performance.now(); let assertMs = 0; const steps: StepTiming[] = [];
  for (const [i, step] of s.tools.entries()) {
    if (step.kind === "bridge") { const b = performance.now(); await step.run(page); t0 += performance.now() - b; continue; } // untimed
    if (step.kind === "tool") {
      const a = performance.now();
      const { execMs } = await page.evaluate(async ([n, args]) => {
        const s = performance.now(); await window.__aoWebMcp!.callTool(n, args); return { execMs: performance.now() - s };
      }, [step.name, step.args ?? {}] as const);
      steps.push({ index: i, tool: step.name, execMs, roundTripMs: performance.now() - a });
    } else { const a = performance.now(); await runExpect(page, host, step); assertMs += performance.now() - a; }
  }
  return { pass: true, totalMs: performance.now() - t0, assertMs, steps };
};

// drivers/playwright.ts
export const playwright: Driver = async (page, host, s) => {
  await s.playwrightReady(page, host);
  const t0 = performance.now();
  const { bridgeMs, assertMs } = await s.playwright(page, host);
  return { pass: true, totalMs: performance.now() - t0 - bridgeMs, assertMs, steps: [] };
};
```

- Timed window (all drivers): starts after `setup` + `goto` + readiness
  (tools listed, or for Playwright the first `expect(...).toBeVisible()` the
  original spec starts with), ends after the last assertion. `bridge` steps
  subtract their own duration.
- Bench spec:

```ts
// frontend/e2e/webmcp-bench.spec.ts
test.skip(process.env.AO_WEBMCP_BENCH !== "1", "set AO_WEBMCP_BENCH=1");
test.describe.configure({ mode: "default", retries: 0 });
const repeats = Number(process.env.AO_WEBMCP_REPEATS ?? 6);
const drivers = { playwright, "webmcp-scripted": scripted, ...(jevAvailable ? { "webmcp-jev": jev } : {}) };
for (const s of scenarios) for (const [name, run] of Object.entries(drivers)) for (let r = 0; r < repeats; r++)
  test(`${s.id} · ${name} · ${r}`, async ({ page, browser }, testInfo) => {
    const host = {}; await s.setup(page, host); await page.goto(s.route);
    const result = await run(page, host, s); // drivers retain timing and partial steps on failure
    const record = buildRecord({ scenario: s, driver: name, repeat: r, result, browser, testInfo });
    validateRecord(record); await writeRecord(record); expect(result.pass, result.error).toBe(true);
  });
```

- Run independent tests with `--workers=1 --retries=0`, not a Playwright
  serial group: a failed attempt must not skip later scenarios or repeats.
  Each driver catches execution/assertion failures inside its timed window
  and returns elapsed time, completed steps, and the error. Never replace a
  failed measurement with zero duration. Write the record before failing
  the test assertion. Setup failures are reported separately.
- Warm-up: the first repeat of each scenario per driver runs but is written
  with `warmup: true`; the summarizer drops it.
- Overhead series: scenario `overhead.evaluate-noop` calls a `noop` tool
  registered by the benchmark fixture 50 times; recorded like any scenario.

## Subtasks

- [ ] `drivers/types.ts`, `drivers/scripted.ts`, `drivers/playwright.ts`.
- [ ] `webmcp-bench.spec.ts` with `AO_WEBMCP_BENCH`, `AO_WEBMCP_REPEATS`,
  `AO_PERF_LABEL`, `AO_PERF_DIR`; `buildRecord` reusing the field code from
  `chat-performance.spec.ts:41-58`, extended per WM-3.1.1.
- [ ] Test-only `noop` fixture registration (removed after the run) and the
  `overhead.evaluate-noop` scenario. Do not register a production noop tool.
- [ ] Record `warmup` flag on repeat 0.

## Acceptance criteria

- [ ] All ten scenarios pass under `playwright` and `webmcp-scripted`.
- [ ] One JSON file per (scenario, driver, repeat) in `AO_PERF_DIR`, each
  passing `validateRecord`.
- [ ] Spec is skipped without `AO_WEBMCP_BENCH=1`; `make test-e2e` time is
  unchanged.

## Verification

- `AO_WEBMCP_BENCH=1 AO_PERF_DIR=/tmp/wm AO_PERF_LABEL=dev AO_E2E_PORT=5292 CI=true npm --prefix frontend run test:e2e -- e2e/webmcp-bench.spec.ts --workers=1 --retries=0`
- `ls /tmp/wm | wc -l` equals `(10 + 1) × 2 × repeats` without a JEV key.

## Dependencies

WM-2.1.1, WM-3.1.1 (record schema; can be developed in the same PR).
