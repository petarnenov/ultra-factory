# WM-1.1.1 · Task: `document.modelContext` shim and `__aoWebMcp` test surface

**Status:** ⬜ todo — [#471](https://github.com/petarnenov/ultra-factory/issues/471)

**Type:** frontend. **Estimate:** 1 pd. **Story:** WM-1.1.

## Goal

Add `frontend/src/renderer/webmcp/model-context.ts` that installs
`document.modelContext.{registerTool,unregisterTool}` when the browser has no
native implementation, keeps a local registry either way, and exposes
`window.__aoWebMcp.{listTools,callTool}` in dev/e2e builds only.

## Decisions (resolved)

- Own shim, no `@mcp-b/webmcp-polyfill` dependency.
- Tools are **always registered** (all builds). The `__aoWebMcp` call surface
  is **dev/e2e only** (`import.meta.env.DEV || usesPreviewWorkspaceData`,
  see `frontend/src/renderer/lib/preview-mode.ts:1`).
- Target entry point is `document.modelContext` (May 2026 draft);
  `navigator.modelContext` is treated as a legacy alias for native detection
  only.

## Context

- Electron `33.4.11` = Chromium `130.0.6723.191`; native WebMCP starts with
  the Chrome 149 origin trial, so the shim is the only path today.
- Install point: `frontend/src/renderer/main.tsx`, module level before
  `renderApp()` (after the DEV block at lines 45-84), so every route mounts
  after the registry exists.
- Validation: the renderer has no JSON-schema validator; implement a minimal
  checker for `type: object`, `required[]`, and property `type` in
  `string|number|boolean|object|array`. Do not add `ajv`.

## Blueprint

```ts
// frontend/src/renderer/webmcp/model-context.ts
export type JsonSchema = {
  type: "object";
  properties?: Record<string, { type: string; description?: string; enum?: string[] }>;
  required?: string[];
};
export type ToolDescriptor<A = unknown, R = unknown> = {
  name: string;               // "<page>.<verb>", e.g. "board.read"
  description: string;        // one sentence, imperative
  inputSchema: JsonSchema;
  execute: (args: A) => Promise<R> | R;
};
export type ToolSummary = Pick<ToolDescriptor, "name" | "description" | "inputSchema">;

declare global {
  interface Document { modelContext?: ModelContextLike }
  interface Window { __aoWebMcp?: { listTools(): ToolSummary[]; callTool(name: string, args?: unknown): Promise<unknown> } }
}
type ModelContextLike = { registerTool(d: ToolDescriptor): void; unregisterTool(name: string): void };

const registry = new Map<string, ToolDescriptor>();
const native: ModelContextLike | undefined =
  document.modelContext ?? (navigator as { modelContext?: ModelContextLike }).modelContext;

export function registerTool(d: ToolDescriptor): () => void { /* set; native?.registerTool(d); return () => unregisterTool(d.name) */ }
export function unregisterTool(name: string): void { /* delete; native?.unregisterTool(name) */ }
export function listTools(): ToolSummary[] { /* sorted by name */ }
export async function callTool(name: string, args: unknown = {}): Promise<unknown> {
  const tool = registry.get(name);
  if (!tool) throw new ToolError("unknown_tool", name);
  const problem = validateArgs(tool.inputSchema, args);
  if (problem) throw new ToolError("invalid_args", `${name}: ${problem}`);
  return tool.execute(args);
}
export function installModelContext(): void {
  if (!native) document.modelContext = { registerTool: (d) => void registerTool(d), unregisterTool };
  if (import.meta.env.DEV || usesPreviewWorkspaceData) window.__aoWebMcp = { listTools, callTool };
}
```

```ts
// frontend/src/renderer/webmcp/useModelContextTool.ts
export function useModelContextTool<A, R>(descriptor: ToolDescriptor<A, R> | null, deps: unknown[]): void {
  useEffect(() => { if (!descriptor) return; return registerTool(descriptor); }, deps);
}
```

- `ToolError` carries `code: "unknown_tool" | "invalid_args" | "precondition"`
  and is what tools throw when the UI state does not allow the action.
- Registering an existing name replaces it and `console.warn`s in DEV.

## Subtasks

- [ ] `model-context.ts` with the registry, native detection and forwarding,
  `validateArgs`, `ToolError`, `installModelContext`.
- [ ] `useModelContextTool.ts` hook.
- [ ] Call `installModelContext()` in `main.tsx` before `renderApp()`.
- [ ] Global type augmentation in `frontend/src/renderer/webmcp/global.d.ts`.
- [ ] Vitest `model-context.test.ts`: register/replace/unregister, native
  forwarding with a stubbed `document.modelContext`, `invalid_args`,
  `unknown_tool`, surface gating (DEV true/false).
- [ ] Playwright `frontend/e2e/webmcp-shim.spec.ts` @P0: on `/#/` after
  `installFakeAgent`, `page.evaluate(() => window.__aoWebMcp!.listTools())`
  returns an array.

## Acceptance criteria

- [ ] `document.modelContext.registerTool` exists in `dev:web` and under the
  packaged app in Playwright; `listTools()` reflects registrations.
- [ ] With a stubbed native `document.modelContext`, calls are forwarded and
  the local registry still answers `listTools`.
- [ ] `callTool` with invalid args rejects before `execute` runs.
- [ ] `window.__aoWebMcp` is `undefined` in a production build
  (`npm run build:web` + a grep for `__aoWebMcp` guarded by the DEV flag).
- [ ] `npm run typecheck`, `npm run typecheck:e2e`, `npm test` pass.

## Verification

- `AO_E2E_PORT=5292 npm --prefix frontend run test:e2e -- e2e/webmcp-shim.spec.ts`
- `make ci-check`

## Dependencies

None.

## Audit constraints — 2026-10-21

- Treat this as an AO benchmark adapter, not a complete implementation of
  the evolving native standard. Feature-detect methods and test native
  registration failures/asynchronous results. The current primary reference
  is [Chrome's imperative API](https://developer.chrome.com/docs/ai/webmcp/imperative-api).
- Registration cleanup must remove only the descriptor it registered: a stale
  cleanup must not delete a newer same-name registration. Test StrictMode,
  replacement, repeated installation, and navigation/unmount during a wait.
- Validate declared enum values, finite numbers and object/array distinctions;
  the minimal checker must not silently accept invalid timeout or section args.
- Grepping a bundle does not prove a global is unavailable. Load production
  web and packaged builds and assert `window.__aoWebMcp === undefined`, with
  preview/test flags off. Test the explicit preview configuration separately.
