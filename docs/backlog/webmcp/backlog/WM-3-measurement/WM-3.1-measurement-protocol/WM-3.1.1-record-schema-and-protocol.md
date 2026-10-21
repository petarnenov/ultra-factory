# WM-3.1.1 · Task: Record schema and measurement protocol

**Status:** ⬜ todo — [#484](https://github.com/petarnenov/ultra-factory/issues/484)

**Type:** tests and documentation. **Estimate:** 0.5 pd. **Story:** WM-3.1.

## Goal

Define the benchmark record type and validator in
`frontend/src/renderer/benchmarks/record.ts` (shared by the e2e bench spec
and the dashboard), and the protocol in `docs/performance/webmcp/PROTOCOL.md`.

## Context (verified)

- Base record: `frontend/e2e/chat-performance.spec.ts:41-58` (`label,
  commit, workload, repeat, browser, node, platform, arch, cpus, cpuModel,
  totalMemoryBytes, viewport, timestamp, result`). Keep those names; add the
  fields below.
- The renderer imports `record.ts` (dashboard) and so does the e2e code
  (`tsconfig.e2e.json` must include `src/renderer/benchmarks/record.ts`;
  check `include` there).

## Blueprint

```ts
// frontend/src/renderer/benchmarks/record.ts
export type Driver = "playwright" | "webmcp-scripted" | "webmcp-jev";
export type BenchStep = { index: number; tool: string; execMs: number; roundTripMs: number;
  decisionMs?: number; choice?: string; confidence?: number; matchedScripted?: boolean };
export type BenchRecord = {
  schema: 1;
  label: string; commit: string; timestamp: string;
  env: { electron?: string; chromium: string; node: string; platform: string; arch: string;
         cpus: number; cpuModel: string; totalMemoryBytes: number; viewport: string };
  scenario: string;                   // "board.card-ready" | ... | "overhead.evaluate-noop"
  page: "board" | "settings" | "chat" | "overhead";
  driver: Driver;
  repeat: number; warmup: boolean;
  pass: boolean; error?: string;
  totalMs: number;                    // timed window, bridge steps excluded
  steps: BenchStep[];                 // [] for playwright
  jev?: { model: string; inputTokens: number; outputTokens: number;
          attempts: { status: number; ms: number }[] };
};
export function validateRecord(r: unknown): asserts r is BenchRecord { /* explicit checks, throws Error listing missing fields */ }
export const DRIVERS: Driver[] = ["playwright", "webmcp-scripted", "webmcp-jev"];
```

- `env.chromium` from `browser.version()`; `env.electron` only when run under
  the Electron config (`test/e2e-pod/playwright.electron.config.ts`), else
  omitted.
- Aggregation contract (used by WM-3.1.2's summarizer and WM-4.1.2's view):

```ts
export type BenchSummaryRow = { scenario: string; page: string; driver: Driver; n: number; passRate: number;
  medianMs: number; minMs: number; maxMs: number; ratioToPlaywright: number | null;
  jev?: { medianDecisionMs: number; medianExecMs: number; medianAssertMs: number; medianSteps: number; matchRate: number } };
export function summarize(records: BenchRecord[]): BenchSummaryRow[];   // pure; drops warmup; keeps failed repeats in n and passRate, excludes them from medians
```

## PROTOCOL.md contents

1. Timed window per driver (as in WM-2.2.1); what is excluded (browser
   launch, dev server, `setup`, `goto`, readiness wait, bridge steps).
2. Repeats: `AO_WEBMCP_REPEATS=6` (1 warm-up + 5 measured), serial,
   `--workers=1`, `CI=true` (no server reuse), one machine, nothing else
   running, laptop on mains.
3. Overhead series: `overhead.evaluate-noop` = 50 `noop` calls; report its
   median per call and show it as a reference line, never subtracted.
4. Statistics: median and min–max over measured repeats; `ratioToPlaywright`
   = driver median ÷ Playwright median for the same scenario.
5. JEV: record `model`, tokens, attempts; a 429 retried once; cap = 3× scripted
   steps.
6. Reproduce commands (the two from WM-2.2.1 and WM-2.2.2).

## Subtasks

- [ ] `record.ts` with types, `validateRecord`, `summarize`, `DRIVERS`.
- [ ] Vitest `record.test.ts`: one valid record; missing `driver`,
  `env.chromium`, `steps` → throws; `summarize` drops warmup, counts failed
  repeats in `n`/`passRate`, computes `ratioToPlaywright`.
- [ ] `tsconfig.e2e.json` includes `record.ts`.
- [ ] `docs/performance/webmcp/PROTOCOL.md` as above.

## Acceptance criteria

- [ ] `validateRecord` rejects the three invalid fixtures and accepts the
  valid one.
- [ ] `summarize` is pure and deterministic (unit test with a fixed fixture
  and a snapshot of the output).
- [ ] `PROTOCOL.md` lets a second developer run WM-3.1.2 without asking.

## Verification

- `npm --prefix frontend test -- src/renderer/benchmarks`
- `npm --prefix frontend run typecheck && npm --prefix frontend run typecheck:e2e`

## Dependencies

None (parallel with WM-2).
