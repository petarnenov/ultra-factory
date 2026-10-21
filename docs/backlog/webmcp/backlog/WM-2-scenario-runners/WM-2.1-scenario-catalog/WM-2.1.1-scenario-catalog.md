# WM-2.1.1 · Task: Scenario catalog and shared assertion layer

**Status:** ⬜ todo — [#478](https://github.com/petarnenov/ultra-factory/issues/478)

**Type:** tests. **Estimate:** 1 pd. **Story:** WM-2.1.

## Goal

Create `frontend/e2e/webmcp/scenarios.ts` (data) and
`frontend/e2e/webmcp/assert.ts` (shared assertions) so the three drivers run
the same steps with the same checks.

## Decisions (resolved)

- The Playwright driver is a **copy of the original spec's steps inside the
  bench spec** (same locators, same expects), not a re-run of the original
  file. Original assertions and behavior stay unchanged; shared setup
  helpers may be extracted as described below.
- Scenarios that need a test-bridge push (`__aoFakeAgent.setStatus`,
  `__aoFakeUpdates.setStatus`) are **included**; the push is a `bridge` step
  executed identically by all drivers and excluded from the timed window.

## Scenario list (10)

| id | page | source spec · title | steps (tool driver) |
|---|---|---|---|
| `board.columns` | board | `smoke-t0.spec.ts:161` "board renders all status columns" (route `/#/projects/ao-demo`, no bridge) | `board.read` → expect 4 columns in order `building, validating, needs_review, ready` |
| `board.card-needs-review` | board | `smoke-board.spec.ts:16` BRD-002 | setup `installFakeAgent({workers:[{id:"mover",title:"Wandering worker",status:"working"}]})`; `board.read` expect `mover` in `building`; **bridge** `setStatus("mover","needs_input","waiting_input")`; `board.wait_for_card{mover,needs_review}`; `board.read` expect `building` has 0, card `attention=true` |
| `board.card-ready` | board | `smoke-board.spec.ts:33` BRD-006 | worker `live`; `board.read` expect `live` in `building`; **bridge** `setStatus("live","mergeable","idle")`; `board.wait_for_card{live,ready}` |
| `board.new-card` | board | `smoke-sessions.spec.ts:21` SES-002 | no workers; `board.read` expect no `fake-spawn`; **bridge** `createWorker({id:"fake-spawn",title:"Spawning worker",activity:"exited"})`; `board.wait_for_card{fake-spawn,building}` |
| `settings.sections` | settings | `smoke-t0.spec.ts:197` SET-001 | goto `/#/settings`; `settings.select_section{updates}`; `settings.read` expect `activeSection=updates`; `settings.select_section{help}`; expect `help` |
| `settings.update-downloaded` | settings | `updates-settings.spec.ts:4` test 1 (state assertions only; geometry checks excluded) | `installFakeBridge({version:"0.12.7-nightly…", updateStatus:{state:"downloaded",version:"0.12.8-nightly…"}, updateSettings:{enabled:true,channel:"nightly",nightlyAck:true}})`; `settings.select_section{updates}`; `settings.read` expect `updates.state=downloaded`, `appVersion` |
| `settings.update-live-flow` | settings | `updates-settings.spec.ts:49` @P0 test 2 | `installFakeBridge({version:"1.0.0", updateStatus:{state:"available",version:"2.0.0"}})`; `settings.select_section{updates}`; `settings.update.download`; `settings.wait_for_update_state{downloading}`; **bridge** `__aoFakeUpdates.setStatus({state:"downloading",percent:42,…})`; `settings.read` expect `percent=42`; **bridge** `setStatus({state:"preparing",…})`; `wait_for_update_state{preparing}`; **bridge** `setStatus({state:"downloaded",version:"2.0.0"})`; `wait_for_update_state{downloaded}` |
| `chat.suggestion-accept` | chat | `chat-prompt-suggestion.spec.ts:69` "Tab inserts…" (color/screenshot checks excluded) | `openChat` route stubs; `chat.wait_for{suggestion_visible}`; `chat.read` expect `composer.text=""`; `chat.send` expect `sent=[]` (empty draft is a no-op); `chat.accept_suggestion`; `chat.read` expect `composer.text=prompt`, `suggestion=null`; `chat.type{text: prompt+" Include the fix in this PR."}`; `chat.send`; expect `sent=[…]` |
| `chat.suggestion-dismiss` | chat | `chat-prompt-suggestion.spec.ts:102` "Escape dismisses…" | `chat.wait_for{suggestion_visible}`; `chat.dismiss_suggestion`; `chat.read` expect `suggestion=null`, `composer.text=""`; expect `sent=[]` |
| `chat.retry-refused` | chat | `chat-retry.spec.ts` "retry refusal is visible…" (`running=false`) | `installRetryConversation(page,false)`; `chat.retry{turn-failed}`; `chat.wait_for{retry_error_visible, value:"stop the current turn before retrying this one"}` |

Excluded, with reasons recorded in the README: `chat-retry` "retry stays
disabled while another turn is running" (a disabled button is a DOM
property; the tool equivalent is a `precondition` throw and is covered by the
WM-1.2.3 unit test, not benchmarked); the two geometry assertions in
`updates-settings` test 1; "long suggestion wraps", "full-length scroll",
"narrow card status truncates" (layout-only); `smoke-board` "SSE pushes card
updates" is covered by `board.card-ready`.

## Blueprint

```ts
// frontend/e2e/webmcp/scenarios.ts
export type Step =
  | { kind: "tool"; name: string; args?: Record<string, unknown> }
  | { kind: "bridge"; run: (page: Page) => Promise<void>; label: string }   // untimed
  | { kind: "expect"; read: string; path: string; equals: unknown }        // callTool(read) then deep-equal at path
  | { kind: "expect-host"; label: string; check: (h: HostState) => void }; // e.g. sent[] captured by page.route

export type Scenario = {
  id: string; page: "board" | "settings" | "chat"; route: string;
  source: { file: string; title: string };
  setup: (page: Page, host: HostState) => Promise<void>;   // installFakeAgent / installFakeBridge / page.route
  tools: Step[];                                           // scripted + JEV
  playwrightReady: (page: Page, host: HostState) => Promise<void>; // untimed readiness
  playwright: (page: Page, host: HostState) => Promise<{ bridgeMs: number; assertMs: number }>;
  // Arguments belong to a phase/action, since one tool can be called with different arguments.
  jevActions: { id: string; phase: string; tool: string; args: Record<string, unknown> }[];
  jevGoal: string;                                         // one sentence
};
export const scenarios: Scenario[] = [...];
```

```ts
// frontend/e2e/webmcp/assert.ts
export async function runExpect(page: Page, host: HostState, step: Step): Promise<void> { ... } // uses Playwright expect
export async function callTool(page: Page, name: string, args?: unknown): Promise<unknown> {
  return page.evaluate(([n, a]) => window.__aoWebMcp!.callTool(n, a), [name, args] as const);
}
export async function waitForTools(page: Page, prefix: string): Promise<void> { /* poll listTools() until a name startsWith prefix */ }
```

- `HostState` holds what the original specs capture in closures (`sent[]`
  from `page.route` on `/conversation/messages`).
- `openChat` (chat-prompt-suggestion.spec.ts:10-67) and
  `installRetryConversation` (chat-retry.spec.ts:64-106) are moved to
  `e2e/support/open-chat.ts` and `e2e/support/retry-conversation.ts` and
  imported by both the original specs and the catalog (no behavior change).

## Subtasks

- [ ] Extract `openChat` and `installRetryConversation` to `e2e/support/`.
- [ ] Write the ten scenarios with `tools`, `playwrightReady`, `playwright`, `jevActions`, `jevGoal`.
- [ ] Write `assert.ts` (`callTool`, `waitForTools`, `runExpect`).
- [ ] `frontend/e2e/webmcp/README.md`: step grammar, the table above, the
  exclusions.
- [ ] `e2e/webmcp-catalog.spec.ts` @P0: for each scenario, run `setup`, goto
  `route`, `waitForTools`, assert every `tool` step name is in `listTools()`.

## Acceptance criteria

- [ ] Ten scenarios; each `source` test exists and passes unchanged.
- [ ] `webmcp-catalog.spec.ts` passes (all tool names registered on the
  scenario's route).
- [ ] `npm run typecheck:e2e` passes; `scenarios.ts` imports tool arg types
  from `src/renderer/webmcp/tools/*`.

## Verification

- `npm --prefix frontend run test:e2e -- e2e/webmcp-catalog.spec.ts e2e/chat-prompt-suggestion.spec.ts e2e/chat-retry.spec.ts`

## Dependencies

WM-1.2.1, WM-1.2.2, WM-1.2.3.

## Audit constraints — 2026-10-21

The table contains ten scenarios (four board, three settings, three chat).
The source specs at `dev` `211848e06` contain the named tests. The
`settings.sections` scenario selects `updates` and then `help` with the same
tool, so arguments cannot be keyed by tool name alone. Define ordered phases
with action candidates, intermediate assertions, and external bridge steps.
All drivers must validate each intermediate state before advancing to the
next phase, and exclude only the measured bridge-call duration from timing.
Keep post-bridge UI settling and assertions inside the timed window.
