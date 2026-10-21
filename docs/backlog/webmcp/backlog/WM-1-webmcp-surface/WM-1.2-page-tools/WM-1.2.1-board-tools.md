# WM-1.2.1 · Task: Board tools

**Status:** ⬜ todo — [#473](https://github.com/petarnenov/ultra-factory/issues/473)

**Type:** frontend. **Estimate:** 1 pd. **Story:** WM-1.2.

## Goal

Register WebMCP tools in `SessionsBoard` that let a driver read the board,
wait for a card to reach a column, open a card, and request a new task, which
covers every board scenario in the WM-2.1.1 catalog.

## Context (verified)

- Board component: `frontend/src/renderer/components/SessionsBoard.tsx:84`
  (`export function SessionsBoard`), root `data-testid="board"` (line 233).
  Columns and cards come from `SessionsBoardGridView`
  (`packages/product-ui/src/SessionsBoardView.tsx:137`), column section
  `data-testid="board-column" data-column=<id>` (lines 196-201), card
  `data-testid="board-session-card" data-session-id=<id>` (line 335).
- Data: `useWorkspaceQuery()` (`hooks/useWorkspaceQuery.ts:743`, key
  `["workspaces"]`). Under `dev:web` it reads `window.__aoFakeAgent.snapshot()`
  (lines 314-335). Sessions are filtered per project and `workerSessions()`
  at `SessionsBoard.tsx:101-112`; archived excluded (line 72); `no_signal`
  forced to `building` (lines 132-136).
- Column order: `boardKanbanColumnOrder = ["building","validating",
  "needs_review","ready"]` (`packages/product-ui/src/session-presentation.ts:336-341`);
  labels `column.<id>` ("Building", "Validating", "In review", "Ready").
- **There is no UI action that moves a card.** The lane is the daemon's
  `kanbanColumn`; e2e moves cards via `window.__aoFakeAgent.setStatus(id,
  status, activity)` (`e2e/support/fake-bridge.ts:829`). So the catalog models
  the move as an external bridge step, and the board tools only observe.
- Open a card: `openSession` at `SessionsBoard.tsx:176`.
- New task: `openNewTask` → `useUiStore.getState().requestNewTask(projectId,
  hostId)` (`hooks/useProjectOrchestratorAction.ts:139-141`,
  `stores/ui-store.ts:645`); the dialog is `GlobalNewTaskDialog`
  (`components/GlobalNewTaskDialog.tsx:15-51`). Submitting a task is
  `TaskComposer.createTask` (`components/TaskComposer.tsx:292`), which POSTs
  `/api/v1/orchestrators/delegate`; it is **not** exposed as a tool in this
  initiative (not in any selected scenario).

## Blueprint

```ts
// frontend/src/renderer/webmcp/tools/board.ts
export type BoardCard = { id: string; title: string; status: string; column: string; attention: boolean };
export type BoardRead = { projectId: string; columns: { id: string; label: string; cards: BoardCard[] }[] };

export function boardTools(ctx: {
  projectId: string;
  columns: KanbanColumnView[];          // SessionsBoard.tsx:94 (getKanbanColumnView)
  sessions: SessionSummary[];           // filtered, line 101-112
  openSession: (id: string) => void;    // line 176
  requestNewTask: () => void;           // useProjectOrchestratorAction openNewTask
  subscribe: (cb: () => void) => () => void; // queryClient.getQueryCache().subscribe for ["workspaces"]
}): ToolDescriptor[] {
  return [
    { name: "board.read", description: "Return board columns and their cards.", inputSchema: { type: "object" },
      execute: () => read(ctx) },
    { name: "board.wait_for_card",
      description: "Resolve when a card is in the given column, or fail after timeoutMs.",
      inputSchema: { type: "object", required: ["sessionId", "column"],
        properties: { sessionId: { type: "string" }, column: { type: "string", enum: boardKanbanColumnOrder }, timeoutMs: { type: "number" } } },
      execute: ({ sessionId, column, timeoutMs = 5000 }) => waitFor(ctx, s => cardIn(s, sessionId, column), timeoutMs) },
    { name: "board.open_session", description: "Open a session card.", inputSchema: {..., required: ["sessionId"]},
      execute: ({ sessionId }) => { ctx.openSession(sessionId); return { opened: sessionId }; } },
    { name: "board.request_new_task", description: "Open the New task dialog for this project.",
      inputSchema: { type: "object" }, execute: () => { ctx.requestNewTask(); return { opened: true }; } },
  ];
}
```

- Registration: in `SessionsBoard`, `useModelContextTool`s per descriptor, deps
  `[projectId, columns, sessions]`. `waitFor` re-reads from
  `queryClient.getQueryData(workspaceQueryKey)` on each cache event, so it
  sees SSE-driven refetches without React re-render.
- `BoardCard.column` is the derived view column (after the `no_signal` →
  `building` rule), identical to what the DOM shows.

## Subtasks

- [ ] `tools/board.ts` with the four descriptors and pure `read()`.
- [ ] Wire in `SessionsBoard.tsx` after the column views are computed.
- [ ] Vitest: `read()` shape from a fixture snapshot; `wait_for_card` resolves
  after a cache update and rejects on timeout with `ToolError("precondition")`.
- [ ] e2e `webmcp-board.spec.ts` @P0: with `installFakeAgent({workers:[{id:"mover",status:"working"}]})`,
  `callTool("board.read")` lists `mover` in `building`; after
  `__aoFakeAgent.setStatus("mover","mergeable","idle")`,
  `callTool("board.wait_for_card",{sessionId:"mover",column:"ready"})`
  resolves.

## Acceptance criteria

- [ ] `listTools()` on `/#/projects/fake-proj` contains exactly the four
  `board.*` tools; on `/#/settings` none.
- [ ] `board.read` columns equal the DOM columns' `data-column` order and the
  card sets per column (asserted in the e2e spec).
- [ ] No change to `smoke-board.spec.ts`, `smoke-sessions.spec.ts`,
  `smoke-t0.spec.ts` behavior.

## Verification

- `npm --prefix frontend run test:e2e -- e2e/smoke-board.spec.ts e2e/smoke-sessions.spec.ts e2e/webmcp-board.spec.ts`
- `make ci-check`

## Dependencies

WM-1.1.1.
