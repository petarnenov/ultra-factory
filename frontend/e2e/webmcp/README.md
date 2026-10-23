# WebMCP scenario catalog

The catalog in `scenarios.ts` is the shared source for scripted, Playwright,
and JEV runs. Each scenario defines ordered phases. A phase may run a bridge
push, then tool actions, then checks before the next phase starts.

## Step grammar

| Kind | Purpose | Timing |
| --- | --- | --- |
| `tool` | Call a registered page tool with optional arguments. | Included |
| `bridge` | Push external test state through the fake bridge. | Excluded |
| `expect` | Read a tool result and compare the value at a dot path. | Included |
| `expect-host` | Check host state captured by a request stub. | Included |

The Playwright driver follows the equivalent original UI interaction. It checks
each intermediate state before advancing. Only the bridge call duration is
excluded; settling and assertions after a bridge remain timed. `jevActions`
give phase-scoped tool candidates so repeated calls to a tool can use different
arguments.

## Scenarios

| ID | Page | Source | Coverage |
| --- | --- | --- | --- |
| `board.columns` | board | `smoke-t0.spec.ts` — board renders all status columns | Four columns and their order |
| `board.card-needs-review` | board | `smoke-board.spec.ts` — BRD-002 | Bridge worker into needs review and verify attention |
| `board.card-ready` | board | `smoke-board.spec.ts` — BRD-006 | Bridge worker into ready |
| `board.new-card` | board | `smoke-sessions.spec.ts` — SES-002 | Bridge a newly created worker onto the board |
| `settings.sections` | settings | `smoke-t0.spec.ts` — SET-001 | Select updates, then help |
| `settings.update-downloaded` | settings | `updates-settings.spec.ts` — downloaded update | Verify downloaded state and app version |
| `settings.update-live-flow` | settings | `updates-settings.spec.ts` — live update flow | Download, then bridge downloading, preparing, and downloaded states |
| `chat.suggestion-accept` | chat | `chat-prompt-suggestion.spec.ts` — Tab inserts suggestion | Accept, edit, and send; empty send is a no-op |
| `chat.suggestion-dismiss` | chat | `chat-prompt-suggestion.spec.ts` — Escape dismisses suggestion | Dismiss without sending |
| `chat.retry-refused` | chat | `chat-retry.spec.ts` — retry refusal is visible | Retry refusal is exposed in chat state |

## Exclusions

- Retry disabled while another turn runs is a DOM property. The tool reports a
  precondition error, covered by the WM-1.2.3 unit test, and is not benchmarked.
- The two geometry assertions in the downloaded update source test are layout
  checks and are not part of scenario state coverage.
- Long suggestion wrapping, full-length suggestion scrolling, and narrow-card
  status truncation are layout-only checks.
- The smoke-board SSE card update is covered by `board.card-ready`.
- Suggestion color and screenshot checks are visual-only. The full-length
  suggestion and narrow viewport variants are likewise layout-only.
