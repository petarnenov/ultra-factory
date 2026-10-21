# WM-2.2.2 · Task: JEV driver

**Status:** ⬜ todo — [#481](https://github.com/petarnenov/ultra-factory/issues/481)

**Type:** tests. **Estimate:** 1–1.5 pd. **Story:** WM-2.2.

## Goal

Add `frontend/e2e/webmcp/drivers/jev.ts`: at each step the TypeSafe `jev`
model chooses the next tool from the route's registered tools plus `done`;
the driver executes the chosen phase action with its fixed catalog arguments, and the loop ends on
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
- JEV does not generate strings: every tool call uses the selected phase action’s catalog arguments
  verbatim. Distinct action IDs may map to the same tool with different arguments.

## Blueprint

The driver follows the shared scenario phases from WM-2.1.1:

1. Create the client with the options above, wait for the page tools, and
   start the measurement clock. Keep accumulated bridge time separately.
2. In each phase, offer JEV the currently valid catalog action IDs plus
   `done`, with tool descriptions and fixed arguments visible in the state.
   Resolve the selected ID to its tool and arguments; do not key arguments
   solely by tool name (`settings.select_section` selects both updates and help).
3. Record every decision, including `done`, with model, tokens, duration and
   HTTP attempts before branching on the chosen action. Execute tools through
   `page.evaluate`, recording browser execution and round-trip durations.
4. Run that phase's assertions before advancing. Initial/intermediate checks
   (empty composer, building column, update percent) cannot be deferred until
   the final state. Run each external bridge step exactly once at its phase
   boundary and accumulate its duration for exclusion by all drivers.
5. `done` succeeds only after every required phase and final assertion has
   passed. Early `done`, an invalid action, a tool/assertion error, or the
   bounded action cap produces a failed record with elapsed time and partial
   evidence. Include a terminal decision allowance for read-only scenarios;
   never derive a zero cap from the count of explicit `tool` steps alone.
6. Return total elapsed time minus bridge time, step records, and JEV metadata.
   Track assertion duration explicitly for the summary's assertion breakdown.

`withOneRetry` retries only 429/5xx once, with a bounded wait and timeout;
it records both attempts and retry waiting in decision time. A missing HTTP
response needs a nullable status, not a fabricated success or failure code.
No test-level retry reruns the entire scenario automatically.

- Prompt text and criteria strings are fixed in code so token counts are
  comparable across repeats.

## Subtasks

- [ ] Add `@typesafe-ai/sdk` to `frontend/devDependencies` (pinned).
- [ ] `drivers/jev.ts` per the blueprint; `jevAvailable` export for the bench
  spec.
- [ ] Record `jev: { model, inputTokens, outputTokens, attempts: [{status,
  ms}] }` and per-step `decisionMs/choice/confidence/matchedScripted`.
- [ ] Unit test the loop with a stubbed client (choices scripted): matched
  path passes; a `done` at step 0 fails incomplete phases; cap reached → `pass=false`.

## Acceptance criteria

- [ ] With the key, all ten scenarios produce a JEV record each, pass or
  fail; failures carry `error` and do not skip later attempts. Test-level
  retries are disabled; the bounded HTTP retry above remains supported.
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
