# RW-2.1.2 · Task: Base URL without `127.0.0.1` and CSP for the web build

**Type:** frontend TS + Vite. **Estimate:** 0.5–1 pd. **Story:** RW-2.1.

## Context
- `frontend/src/renderer/lib/api-client.ts:12-26`: `VITE_AO_API_BASE_URL` takes precedence; an empty string = same-origin; `muxUrlFromApiBase("")` uses `window.location` (`terminal-mux.ts:72-77`).
- `frontend/src/renderer/lib/daemon-status.ts:8-11`: forces `http://127.0.0.1:${port}` on ready.
- `frontend/vite.renderer.csp.ts:80-86`: `connect-src` is pinned to `http://127.0.0.1:*`, `ws://127.0.0.1:*`.
- SSE sites: `event-transport.ts:437,467`, `notifications.ts:650`, `workspace-file-events.ts:202`, `chat/IndependentSideChats.tsx:513`, `remote-sse-probe.ts:33`. All use the base URL from api-client.

## Subtasks
- [ ] `applyDaemonStatus`: if `import.meta.env.VITE_AO_WEB === "1"` (a new flag for the web target), on ready set the base URL to `""` (same-origin) or to `VITE_AO_API_BASE_URL`; otherwise keep the current behavior.
- [ ] Check that the five SSE sites and `/mux` build their URL from `getApiBaseUrl()`, not from a hardcoded host. Fix any deviations.
- [ ] `vite.renderer.csp.ts`: with `VITE_AO_WEB`, generate `connect-src 'self' wss: https:` restricted to same-origin (`'self'` covers same-origin ws/wss in modern browsers; add `wss://<host>` explicitly if needed). `img-src` without `127.0.0.1`.
- [ ] Tests: `api-client.test.ts` for web mode; a CSP unit test (`vite.renderer.config.test.ts` style).

## Acceptance criteria
- [ ] In the web build, the Network tab shows only same-origin requests; no CSP errors in the console.
