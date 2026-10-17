# RW-2.1.1 · Task: Web `daemon` adapter in `aoBridge`
**Status:** ✅ done (#62)

**Type:** frontend TS. **Estimate:** 1 pd. **Story:** RW-2.1.

## Context
- `frontend/src/renderer/lib/bridge.ts` (351 LOC): a facade with a browser stub when `window.ao` is missing. The `daemon.getStatus()` stub returns `stopped`.
- `frontend/src/renderer/routes/_shell.tsx:557,1044,1088` require `daemonStatus.state === "ready" && port`.
- `frontend/src/renderer/lib/daemon-status.ts:6-13` `applyDaemonStatus`.
- Daemon health: `GET /healthz`, `GET /readyz` (router.go). Through the BFF this is same-origin `/readyz`.

## Subtasks
- [ ] In the `bridge.ts` browser stub: `daemon.getStatus()` does `fetch(<base>/readyz)`; 200 → `{state:"ready", port: <from base URL or 0>}`; error → `{state:"error", message}`; `onStatus` polls every 5 s until ready, then every 30 s; `start/stop/restart` → no-op with `supported:false`.
- [ ] Make sure the `DaemonStatus` type allows a missing/zero `port` in web mode, or pass the port from `window.location.port`.
- [ ] Tests (vitest, jsdom): ready on 200, error on network failure, polling stops on unmount.

## Acceptance criteria
- [ ] In a web build without `VITE_NO_ELECTRON`, `_shell` passes the gate against a running daemon behind a proxy.
