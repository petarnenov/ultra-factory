# WM-2 · Epic: Scenario catalog and drivers

**Status:** ⬜ todo — [#476](https://github.com/petarnenov/ultra-factory/issues/476)

**Goal.** Express the selected Playwright tests as a driver-neutral scenario
catalog, then run that catalog through a Playwright driver (copied steps), a
scripted WebMCP driver, and a JEV driver inside one Playwright harness with
identical assertions and one clock.

**Estimate:** 3–3.5 pd. **Stage:** drivers. **Dependencies:** WM-1.

| Story | Name | Estimate |
|---|---|---:|
| WM-2.1 | Scenario catalog mapped 1:1 from Playwright | 1 pd |
| WM-2.2 | Playwright, scripted, and JEV drivers in one bench spec | 2–2.5 pd |

**Decisions (resolved 2026-10-21)**

- Playwright baseline = the original spec's steps copied into the bench spec
  (same locators and expects); original specs are not modified.
- Scenarios needing a test-bridge push (`__aoFakeAgent.setStatus`,
  `__aoFakeUpdates.setStatus`) are included; the push is an untimed `bridge`
  step shared by all drivers.
- JEV client = `@typesafe-ai/sdk` with `maxRetries: 0`; one manual retry on
  429/5xx recorded in the data.

**Acceptance criteria**

- [ ] Ten catalog scenarios, each naming its source spec and title, with
  steps that are one tool call, one bridge step, or one assertion.
- [ ] `AO_WEBMCP_BENCH=1 npm run test:e2e -- e2e/webmcp-bench.spec.ts` runs
  all drivers per scenario and writes one record per run.
- [ ] Without `AO_WEBMCP_BENCH=1` the bench spec is skipped; without a key
  only the JEV driver is skipped and says why.
- [ ] Only the Playwright driver reads the DOM; the WebMCP drivers assert on
  tool results.

**Risks.** JEV cannot generate strings (arguments come from the catalog);
JEV may pick a wrong tool or loop (capped at 3× scripted steps, recorded as a
failed repeat, never retried silently).
