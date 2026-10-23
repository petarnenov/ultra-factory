# WM-4.1.3 · Task: Performance report under `docs/performance/webmcp/`

**Status:** ✅ complete — [#490](https://github.com/petarnenov/ultra-factory/issues/490)

**Type:** documentation. **Estimate:** 0.5 pd. **Story:** WM-4.1.

## Goal

Write `docs/performance/webmcp/README.md` in the format of
`docs/performance/chat-responsiveness/README.md`, generated from
`summary.json`, with screenshots of the dashboard and the limits of the
comparison.

## Context (verified)

- Template sections (`docs/performance/chat-responsiveness/README.md`):
  scope paragraph; `## What was detected and changed` (replace with
  `## What was compared`); `## Measurement method` (line 16); `## Results`
  (line 27, hardware line + table of medians with ranges, link to
  `measurements.json`); `## Reproduce` (line 47); `## Verification` (line 59,
  Check | Result table); `## Screenshots` (line 87); `## Deliberate limits`
  (line 97).
- The Markdown results table is printed by
  `frontend/scripts/benchmarks-summarize.mjs` (WM-3.1.2); paste it, do not
  retype.

## Blueprint

```markdown
# WebMCP UI testing benchmark

Scope: three pages, ten scenarios, three drivers, one machine. Pre-measurement analysis and sources:
../../backlog/webmcp/webmcp-report.md. Dashboard: `/#/benchmarks`.

## What was compared
| Driver | Mechanism | Decision step |
| playwright | original spec steps (locators) | none |
| webmcp-scripted | catalog tool calls via `__aoWebMcp.callTool` | none |
| webmcp-jev | JEV picks each tool (`jev-<model>`), args from catalog | 1 API call per step |

## Measurement method   ← from PROTOCOL.md: timed window, repeats, overhead series, exclusions
## Results              ← hardware line; pasted table; overhead line; JEV decision/exec split; pass rates
## Reproduce            ← the two commands from WM-2.2.1 / WM-2.2.2 and the summarizer
## Verification         ← Check | Result (summary reproducible; records validate; no key in repo)
## Screenshots          ← assets/benchmarks-dark.png, assets/benchmarks-light.png (from WM-4.1.2 spec)
## Deliberate limits    ← shim not native; page.evaluate overhead (measured value); JEV cannot generate text;
                           JEV early access, no published limits; one machine/session; layout-only scenarios excluded
## Answer: what performance to expect   ← one paragraph citing only measured medians and ratios
```

## Subtasks

- [x] Generate and paste the results table; fill every section.
- [x] Copy screenshots from the WM-4.1.2 spec output to `assets/`.
- [x] Link from `docs/README.md` (performance section) and from
  `docs/backlog/webmcp/webmcp-report.md` ("Results" pointer at the top).
- [x] Update `docs/backlog/webmcp/backlog/README.md` statuses to ✅ for the
  finished tasks.

## Acceptance criteria

- [x] Every measured result traces to raw records or `summary.json`; hardware
  and environment metadata trace to the records’ `env` fields.
- [ ] Reproduce commands work from a clean checkout with the key set; the
  protocol run is still required to verify this acceptance criterion.
- [x] English only; no secret.

## Verification

- A second developer follows the protocol and reports the observed result
  and environment; do not require a new stochastic run to fall inside the
  first sample’s min–max. Record deviations instead of discarding them.

## Dependencies

WM-3.1.2, WM-4.1.2.
