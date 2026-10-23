# WebMCP UI testing benchmark: Playwright vs WebMCP scripted vs WebMCP + JEV

**Date:** 2026-10-21. **Base:** `dev` @ `8a85844fb`.

## Results

The interim performance report and measured results are in
[docs/performance/webmcp](../../performance/webmcp/README.md). The dashboard is
at `/#/benchmarks`. Screenshots and final task status are pending merge of
dashboard PR #592.

## Goal

Expose a WebMCP tool surface on three UI pages that already have Playwright
coverage, re-implement the same test scenarios 1:1 through that surface, and
measure how each driver performs:

1. **Playwright** (existing specs; the baseline).
2. **WebMCP scripted**: the scenario calls the page's registered tools in a
   fixed order, no model in the loop.
3. **WebMCP + JEV**: the TypeSafe `jev` decision model picks the next tool at
   every step from the registered tool list.

The results are recorded as JSON, compared on a new dashboard page with charts,
and summarised in a report under `docs/performance/`.

## 1. Conclusions

- **No public benchmark answers this question.** Nothing compares WebMCP
  (scripted or with JEV) against Playwright on the same scenario. The only way
  to get numbers for AO is to measure them here.
- **Native WebMCP is unavailable in the shipped app.** Electron `33.4.11`
  bundles Chromium `130.0.6723.191`; Chrome's WebMCP origin trial starts at
  Chrome 149 and the API surface is still moving (`navigator.modelContext` →
  `document.modelContext` in the May 2026 draft). The tool surface must be a
  renderer-side shim with the same `registerTool` shape so it can be swapped
  for the native API later.
- **JEV is a classifier, not a text model.** It picks one option out of up to
  255 and returns probabilities and confidence. It cannot generate input
  strings, so every tool call in the JEV runner must be fully parameterised
  by the scenario (fixed argument table) or parameter-free.
- **Decision latency is the only vendor number we have.** TypeSafe reports
  236–276 ms p50 per JEV request (cited in arXiv 2609.28940); LLM baselines in
  the same source are 1.5–3.0 s per decision. Playwright has no decision step.
  Expected ordering before measuring: scripted WebMCP ≤ Playwright <
  WebMCP + JEV. The size of each gap is unknown and is what this initiative
  measures.
- **Apples to apples needs one harness.** All three drivers run inside the
  same Playwright test process, share the same DOM assertions, and use the same
  clock. The WebMCP drivers invoke tools via `page.evaluate`; its overhead is
  measured separately so it can be subtracted.
- **JEV is a network call.** `AGENTS.md` forbids network in tests unless an
  e2e pattern exists, so the JEV runner is opt-in (`AO_WEBMCP_BENCH=1`), never
  part of `make test-e2e` or CI gates. The key is already present on this
  machine as `JEV_MAF_LAB`; the SDK reads `TYPESAFE_API_KEY`, so the runner
  maps one to the other.

## 1a. Decisions taken on 2026-10-21

| Question | Decision |
|---|---|
| Tool registry in Chromium 130 | Own shim (`document.modelContext`), no polyfill dependency |
| When tools register | In all builds; `window.__aoWebMcp` list/call surface in dev/e2e only |
| Playwright baseline timing | Original spec steps copied into the bench spec, same clock as the other drivers |
| Scenarios that need a test-bridge push | Included; the push is an untimed `bridge` step shared by all drivers |
| JEV client | `@typesafe-ai/sdk` 0.7.0 with `maxRetries: 0`; one manual retry on 429/5xx, recorded |
| Dashboard data source | Committed JSON in `frontend/src/renderer/benchmarks/data/`; no daemon API |
| Charts | `recharts` via a shadcn `components/ui/chart.tsx`; existing `--chart-1..5` tokens |
| Tracking | GitHub issues #469–#490 (`wm`, `wm:epic/story/task`), nested as sub-issues; table in `backlog/README.md` |

**JEV probe (measured here, 2026-10-21):** three `choice` requests with 3
options against `https://api.typesafe.ai/v1/systemone` using the local key
returned HTTP 200 in **151 / 220 / 242 ms**, model `jev-1.13.0`, 373 input
and 43 output tokens each.

## 2. What exists in the code today

- **Playwright renderer suite** in `frontend/e2e/*.spec.ts`, run by
  `npm --prefix frontend run test:e2e` (`Makefile:120`) against `dev:web` with
  the fake bridge from `frontend/e2e/support/fake-bridge.ts`.
- **An existing benchmark pattern**: `frontend/e2e/chat-performance.spec.ts`
  is gated by `AO_PERF_BENCH=1`, writes one JSON record per run to
  `AO_PERF_DIR` (label, commit, workload, browser, node, platform, CPU, memory,
  viewport, timestamp, result), and `docs/performance/chat-responsiveness/`
  is the report format (method, results table with medians and ranges,
  reproduce command, verification, deliberate limits).
- **Three pages with the deepest existing coverage** (hash routes):
  - Board, `/#/projects/<projectId>` (`components/SessionsBoard.tsx:84`):
    `smoke-board.spec.ts` (BRD-002, BRD-006), `smoke-sessions.spec.ts`
    (SES-002), `smoke-t0.spec.ts` @BRD. **No UI action moves a card**; the
    lane is the daemon's `kanbanColumn` and e2e moves cards through
    `window.__aoFakeAgent.setStatus` (`e2e/support/fake-bridge.ts:829`).
  - Settings, `/#/settings`: **a redirect** (`routes/_shell.settings.tsx:11-21`)
    that opens the global settings overlay; the active section is local
    state in `components/SettingsDialog.tsx:113`, selected by
    `selectGlobalSection` (lines 228-231). Covered by `smoke-t0.spec.ts` @SET
    and `updates-settings.spec.ts` (2 tests, update state driven by
    `window.__aoFakeUpdates.setStatus`).
  - Session chat, `/#/projects/<p>/sessions/<s>` (`components/chat/
    ComposerEditor.tsx`, `hooks/useConversation.ts`):
    `chat-prompt-suggestion.spec.ts` (Tab accept at lines 499-522, Escape
    dismiss at 554-561), `chat-retry.spec.ts` (`retryControl`,
    `useConversation.ts:1117-1145`).
- **Frontend routing**: TanStack file routes under
  `frontend/src/renderer/routes/` (`_shell.<name>.tsx`), sidebar top-nav rows
  in `components/Sidebar.tsx`, shadcn primitives in `components/ui/*`.
- **No chart library and no benchmark page.** `frontend/package.json` has no
  recharts/chart.js/d3; `components/ui/` has no `chart.tsx`.
- **No WebMCP or TypeSafe code or config** anywhere in the repository.

## 3. Options

### A. Shim WebMCP in the renderer, run all drivers inside Playwright (recommended)

- A small `document.modelContext` shim (`registerTool`/`unregisterTool`) that
  defers to the native API when present, plus a test-only
  `window.__aoWebMcp.{listTools,callTool}` surface.
- Each of the three pages registers a handful of tools that wrap the same
  state transitions the Playwright specs exercise.
- A scenario catalog maps each selected Playwright test to a step list; the
  scripted and JEV runners execute the steps via `page.evaluate`, and the
  Playwright runner is the original spec. All share one `expect` layer.
- Dashboard reads committed measurement JSON; no daemon API change.

### B. Use `@mcp-b/webmcp-polyfill` with its testing shim

- Same as A but with a third-party polyfill providing
  `navigator.modelContextTesting`. Fewer lines, one more dependency that
  tracks a spec still being renamed. Reasonable fallback if the own shim
  grows beyond ~100 lines.

### C. Upgrade Electron to a Chromium with native WebMCP

- Chromium 149+ means Electron ≥ 40-class releases. A large, unrelated
  upgrade; out of scope for a measurement initiative. Revisit when the API
  stabilises.

## 4. Plan for option A: complexity and time

| Epic | Content | Estimate |
|---|---|---:|
| WM-1 | WebMCP shim and page tools on Board, Settings overlay, Session chat | 3.5–4 pd |
| WM-2 | Scenario catalog (9 scenarios, 1:1 from Playwright), Playwright/scripted/JEV drivers | 3–3.5 pd |
| WM-3 | Record schema, protocol, measured runs, committed data set | 1.5 pd |
| WM-4 | Benchmarks dashboard page with charts and the written report | 2–2.5 pd |

Total: 10–11.5 pd. Critical path: WM-1.1.1 → WM-1.2.x → WM-2.1.1 → WM-2.2.1 →
WM-2.2.2 → WM-3.1.2 → WM-4.1.2 → WM-4.1.3. Parallel from day one: WM-3.1.1
(schema) and WM-4.1.1 (chart primitive).

## 5. Risks and open questions

- **`page.evaluate` overhead** inflates the WebMCP drivers. Measured and
  reported as a separate series (WM-3.1.1); not subtracted silently.
- **JEV cannot fill text.** Scenarios that type free text (prompt suggestion,
  retry message) must expose tools with fixed payloads; this is recorded as a
  deliberate limit, not hidden.
- **JEV may loop or pick a wrong tool.** The runner caps steps at 3× the
  scripted step count, records pass/fail and per-step confidence, and reports
  pass rate alongside timing.
- **Early-access API.** JEV is behind a waitlist; rate limits and pricing are
  not published. The runner must tolerate 429s and record them.
- **Spec drift.** WebMCP renamed its entry point once already; the shim
  isolates the rename to one file.
- **Tools in production builds.** Decided: registered always. Each tool
  must be a thin wrapper over an existing user action with the UI's own
  guards; no tool returns secrets or raw API payloads. The call surface
  (`__aoWebMcp`) stays dev/e2e only.
- **Board scenarios measure observation, not action.** Because no UI action
  moves a card, the board scenarios time `read`/`wait_for_card` after a
  bridge push; the report must say so.
- **Secrets.** The key stays in the environment; nothing under `docs/` or
  `frontend/` may contain it.

## 6. Recommended next step

Start WM-1.1.1 (shim) and WM-2.1.1 (scenario catalog) in parallel; they fix
the contracts everything else depends on. Measure (WM-3) before building the
dashboard so the page is designed around real records.

## Sources

- Chrome WebMCP origin trial (Chrome 149):
  <https://developer.chrome.com/blog/ai-webmcp-origin-trial>
- WebMCP state and entry-point rename:
  <https://www.spronta.com/blog/state-of-webmcp-july-2026/>
- `@mcp-b/webmcp-polyfill` README:
  <https://cdn.jsdelivr.net/npm/@mcp-b/webmcp-polyfill@2.2.0/README.md>
- TypeSafe quick start and Choice primitive (endpoint, SDK, 255-option limit):
  <https://docs.typesafe.ai/introduction/quickstart>,
  <https://docs.typesafe.ai/primitives/choice>
- JEV latency as cited (236–276 ms p50): <https://arxiv.org/pdf/2609.28940>
- JEV browser agent demo (1 task × 3 runs):
  <https://github.com/browser-use/jev-ultrafast>
- JEV mobile QA proof of concept:
  <https://www.callstack.com/blog/exploring-jev-for-mobile-qa-with-agent-device>
