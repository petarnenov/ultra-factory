# Backlog: WebMCP UI testing benchmark (key `WM`)

Source: `../webmcp-report.md`. Orchestrator instructions: `../orchestrator-prompt.md`.

## Structure

```text
backlog/
  WM-1-webmcp-surface/              epic -> EPIC.md
    WM-1.1-runtime-shim/             story -> STORY.md
      WM-1.1.1-model-context-shim.md task
```

- **Epic** = a delivery phase. **Story** = an operator-visible capability.
  **Task** = one reviewable implementation unit. Subtasks are checklists.
- Every task includes Goal, Context, Subtasks, Acceptance criteria,
  Verification, Estimate, and Dependencies.
- Estimates are person-days for a developer familiar with the repository.
- Status icons follow the [RW backlog legend](../../railway/backlog/README.md#status):
  ✅ done, 👀 in review, 🔄 in progress, ⛔ blocked or needs-human, ⬜ todo.

## Execution order

| Epic | Content | Estimate | Stage |
|---|---|---:|---|
| WM-1 | WebMCP shim and tools on Board, Settings overlay, Session chat | 3.5–4 pd | Surface |
| WM-2 | Scenario catalog, Playwright/scripted/JEV drivers | 3–3.5 pd | Drivers |
| WM-3 | Record schema, protocol, measured runs | 1.5 pd | Measure |
| WM-4 | Benchmarks dashboard page and report | 2–2.5 pd | Publish |

Critical path: WM-1.1.1 → WM-1.2.x → WM-2.1.1 → WM-2.2.1 → WM-2.2.2 →
WM-3.1.2 → WM-4.1.2 → WM-4.1.3. WM-3.1.1 and WM-4.1.1 start in parallel on
day one. Every task is an implementation-ready blueprint: file paths,
handlers with `file:line`, tool contracts, and code sketches are in the task.

## Decisions (resolved 2026-10-21)

See `../webmcp-report.md` §1a. In short: own shim; tools registered in all
builds, call surface dev/e2e only; Playwright baseline copied into the bench
spec; bridge-push scenarios included as untimed steps; `@typesafe-ai/sdk`
with retries off; committed JSON as data source; recharts via shadcn chart.

## Invariants

- The three drivers run the **same nine scenarios with the same assertions**
  inside one Playwright harness; a scenario that cannot be expressed 1:1 is
  dropped from all three, not approximated in one.
- Tool registration goes through one shim module; a native
  `document.modelContext` receives the registrations when present.
- Tools wrap existing user actions with the UI's own guards and never return
  secrets or raw API payloads.
- The bench spec is opt-in via `AO_WEBMCP_BENCH=1`; the JEV driver also needs
  a key. Neither is part of `make test-e2e`, `make ci-check`, or any CI gate.
- The API key is read from `TYPESAFE_API_KEY`, falling back to `JEV_MAF_LAB`.
  It is never written to a file, a record, or a log.
- Every measurement record carries commit, Chromium/Electron version, Node,
  platform, CPU, memory, driver, scenario, repeat index, warm-up flag, and
  per-step timings; bridge steps are untimed.
- Reported numbers are medians with min–max over ≥ 5 measured serial repeats
  on one machine; failed repeats are kept and flagged; the dashboard shows
  the raw repeats as well.

## GitHub issue tracking

Issues mirror the Epic → Story → Task hierarchy, nested as sub-issues, with
labels `wm`, `wm:epic`, `wm:story`, `wm:task` plus exactly one type and one
`priority:*` label per `docs/issue-policy.md`. Each issue body is the
blueprint file. Branch names use `wm/<task-id>`; PRs go to `dev` with
`Closes #<task issue>`.

<!-- status:begin -->
| Item | GitHub issue | Status |
|---|---:|---|
| [WM-1] Epic: WebMCP shim and page tools | [#469](https://github.com/petarnenov/ultra-factory/issues/469) | ⬜ todo |
| [WM-1.1] Story: Runtime shim with a test-only call surface | [#470](https://github.com/petarnenov/ultra-factory/issues/470) | ⬜ todo |
| [WM-1.1.1] Task: `document.modelContext` shim and `__aoWebMcp` test surface | [#471](https://github.com/petarnenov/ultra-factory/issues/471) | ⬜ todo |
| [WM-1.2] Story: Tools on the three benchmark pages | [#472](https://github.com/petarnenov/ultra-factory/issues/472) | ⬜ todo |
| [WM-1.2.1] Task: Board tools | [#473](https://github.com/petarnenov/ultra-factory/issues/473) | ⬜ todo |
| [WM-1.2.2] Task: Settings tools | [#474](https://github.com/petarnenov/ultra-factory/issues/474) | ⬜ todo |
| [WM-1.2.3] Task: Session chat tools | [#475](https://github.com/petarnenov/ultra-factory/issues/475) | ⬜ todo |
| [WM-2] Epic: Scenario catalog and drivers | [#476](https://github.com/petarnenov/ultra-factory/issues/476) | ⬜ todo |
| [WM-2.1] Story: Scenario catalog mapped 1:1 from Playwright | [#477](https://github.com/petarnenov/ultra-factory/issues/477) | ⬜ todo |
| [WM-2.1.1] Task: Scenario catalog and shared assertion layer | [#478](https://github.com/petarnenov/ultra-factory/issues/478) | ⬜ todo |
| [WM-2.2] Story: Playwright, scripted, and JEV drivers in one bench spec | [#479](https://github.com/petarnenov/ultra-factory/issues/479) | ⬜ todo |
| [WM-2.2.1] Task: Scripted WebMCP driver, Playwright driver, and bench spec | [#480](https://github.com/petarnenov/ultra-factory/issues/480) | ⬜ todo |
| [WM-2.2.2] Task: JEV driver | [#481](https://github.com/petarnenov/ultra-factory/issues/481) | ⬜ todo |
| [WM-3] Epic: Measurement protocol and measured runs | [#482](https://github.com/petarnenov/ultra-factory/issues/482) | ⬜ todo |
| [WM-3.1] Story: Record format, protocol, and the measured data set | [#483](https://github.com/petarnenov/ultra-factory/issues/483) | ⬜ todo |
| [WM-3.1.1] Task: Record schema and measurement protocol | [#484](https://github.com/petarnenov/ultra-factory/issues/484) | ⬜ todo |
| [WM-3.1.2] Task: Run the benchmark and commit the data set | [#485](https://github.com/petarnenov/ultra-factory/issues/485) | ⬜ todo |
| [WM-4] Epic: Benchmarks dashboard page and report | [#486](https://github.com/petarnenov/ultra-factory/issues/486) | ⬜ todo |
| [WM-4.1] Story: Benchmarks page with charts and the report | [#487](https://github.com/petarnenov/ultra-factory/issues/487) | ⬜ todo |
| [WM-4.1.1] Task: Chart primitive | [#488](https://github.com/petarnenov/ultra-factory/issues/488) | ⬜ todo |
| [WM-4.1.2] Task: `/#/benchmarks` route, data loader, comparison views | [#489](https://github.com/petarnenov/ultra-factory/issues/489) | ⬜ todo |
| [WM-4.1.3] Task: Performance report under `docs/performance/webmcp/` | [#490](https://github.com/petarnenov/ultra-factory/issues/490) | ⬜ todo |
<!-- status:end -->

## Repo rules every task follows

- Read `DESIGN.md` before any UI change; build from `components/ui/*`.
- Keep daemon logic out of `frontend/`; this initiative touches the renderer
  and e2e only.
- Keep comments to at most two lines; run `make ci-check` before a PR.
- Keep repository documents and comments in English.
- Demo UI changes with `ao preview` from inside the session.
