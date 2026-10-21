# WM-2.2.2 · Task: JEV driver

**Status:** ⬜ todo — [#481](https://github.com/petarnenov/ultra-factory/issues/481)

**Type:** tests. **Estimate:** 1–1.5 pd. **Story:** WM-2.2.

## Goal

Add `frontend/e2e/webmcp/drivers/jev.ts`: at each step the TypeSafe `jev`
model chooses the next tool from the route's registered tools plus `done`;
the driver executes it with the scenario's `jevArgs`, and the loop ends on
`done` or a step cap.

## Decisions (resolved)

- Client: `@typesafe-ai/sdk` (`0.7.0`, Node ≥ 22; this repo runs Node 24).
  Construct `new TypeSafeClient({ retry: { maxRetries: 0 }, timeout: 10000 })`
  so one call is one HTTP round trip; the driver does its **own single retry**
  on `RateLimitError`/5xx and records both attempts.
- Key: `TYPESAFE_API_KEY`, else `JEV_MAF_LAB` (pass it as `apiKey`). Never
  logged; `logLevel: "off"`.
- Opt-in only: `AO_WEBMCP_BENCH=1` **and** a key present; otherwise the driver
  is skipped with a reason. Never part of `make test-e2e` or CI
  (`AGENTS.md` "no network in tests").

## Context (verified)

- API shape: `client.systemOne({ state, questions })` with
  `choice(instructions, criteria)`; response `answers.<id>.{choice,
  confidence, probabilities}` and `usage.{input_tokens, output_tokens}`;
  `model` in the response (`jev-1.13.0` on 2026-10-21).
- Measured on this machine with the local key (3 requests, 3 options):
  HTTP 200 in **151 / 220 / 242 ms**. Vendor-cited p50 is 236–276 ms.
- A Choice question accepts up to 255 options; the per-page tool count is
  4–7 plus `done`.
- JEV does not generate strings: every tool call uses `scenario.jevArgs[tool]`
  verbatim.

## Blueprint

```ts
// frontend/e2e/webmcp/drivers/jev.ts
import { choice, TypeSafeClient, RateLimitError, APIError } from "@typesafe-ai/sdk";

export const jevAvailable = Boolean(process.env.TYPESAFE_API_KEY ?? process.env.JEV_MAF_LAB);

export const jev: Driver = async (page, host, s) => {
  const client = new TypeSafeClient({ apiKey: process.env.TYPESAFE_API_KEY ?? process.env.JEV_MAF_LAB,
    retry: { maxRetries: 0 }, timeout: 10_000, logLevel: "off" });
  await waitForTools(page, `${s.page}.`);
  const tools = await page.evaluate(() => window.__aoWebMcp!.listTools());
  const scriptedOrder = s.tools.filter(t => t.kind === "tool").map(t => t.name);
  const cap = scriptedOrder.length * 3;
  const history: string[] = []; const steps: StepTiming[] = []; let lastRead: unknown = null;
  const t0 = performance.now();
  for (let i = 0; i < cap; i++) {
    // bridge steps fire when the scripted order says they are due (external to the agent)
    await runDueBridgeSteps(s, history, page, t0adjust);
    const criteria = Object.fromEntries([...tools.map(t => [t.name, t.description]), ["done", "The goal is reached; stop."]]);
    const state = JSON.stringify({ goal: s.jevGoal, called: history, lastRead }, null, 0);
    const d0 = performance.now();
    const res = await withOneRetry(() => client.systemOne({ state, questions: { next: choice("Which tool should be called next?", criteria) } }));
    const decisionMs = performance.now() - d0;
    const { choice: name, confidence } = res.answers.next;
    if (name === "done") { steps.push({ index: i, tool: "done", execMs: 0, roundTripMs: 0, decisionMs, choice: name, confidence }); break; }
    const a = performance.now();
    const { execMs, result } = await page.evaluate(async ([n, args]) => { const st = performance.now();
      const r = await window.__aoWebMcp!.callTool(n, args); return { execMs: performance.now() - st, result: r }; }, [name, s.jevArgs[name] ?? {}] as const);
    if (name.endsWith(".read")) lastRead = result;
    history.push(name);
    steps.push({ index: i, tool: name, execMs, roundTripMs: performance.now() - a, decisionMs, choice: name, confidence,
      matchedScripted: scriptedOrder[history.length - 1] === name });
    jevMeta.push({ model: res.model, usage: res.usage });
  }
  let pass = true, error: string | undefined;
  try { for (const e of s.tools.filter(t => t.kind !== "tool" && t.kind !== "bridge")) await runExpect(page, host, e); } catch (e) { pass = false; error = String(e); }
  if (history.length >= cap) { pass = false; error ??= "step cap reached"; }
  return { pass, totalMs: performance.now() - t0, steps, error };
};
```

- `withOneRetry` catches `RateLimitError` or `APIError` with status ≥ 500,
  waits `Retry-After` or 1 s, retries once, and records `httpStatus` of both
  attempts in `record.jev.attempts[]`.
- `runDueBridgeSteps`: a bridge step in `s.tools` at position *k* fires once
  the agent has called the tool at position *k − 1* (so "setStatus" happens
  after the first `board.read`, as in the original spec). Bridge time is
  subtracted from `totalMs`.
- Prompt text and criteria strings are fixed in code so token counts are
  comparable across repeats.

## Subtasks

- [ ] Add `@typesafe-ai/sdk` to `frontend/devDependencies` (pinned).
- [ ] `drivers/jev.ts` per the blueprint; `jevAvailable` export for the bench
  spec.
- [ ] Record `jev: { model, inputTokens, outputTokens, attempts: [{status,
  ms}] }` and per-step `decisionMs/choice/confidence/matchedScripted`.
- [ ] Unit test the loop with a stubbed client (choices scripted): matched
  path passes; a `done` at step 0 fails the expects; cap reached → `pass=false`.

## Acceptance criteria

- [ ] With the key, all nine scenarios produce a JEV record each, pass or
  fail; failures carry `error` and are not retried.
- [ ] `decisionMs` is recorded separately from `execMs`/`roundTripMs`.
- [ ] Without a key, the bench spec runs the other two drivers and prints the
  skip reason once.
- [ ] `git grep` for the key value finds nothing; records contain no header
  or key material.

## Verification

- `AO_WEBMCP_BENCH=1 TYPESAFE_API_KEY=$JEV_MAF_LAB AO_PERF_DIR=/tmp/wm AO_E2E_PORT=5292 CI=true npm --prefix frontend run test:e2e -- e2e/webmcp-bench.spec.ts --workers=1 --grep webmcp-jev`
- `grep -rl "$JEV_MAF_LAB" /tmp/wm` prints nothing.

## Dependencies

WM-2.2.1.
