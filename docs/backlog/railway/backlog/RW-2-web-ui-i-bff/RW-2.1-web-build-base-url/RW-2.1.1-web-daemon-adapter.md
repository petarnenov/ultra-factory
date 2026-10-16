# RW-2.1.1 · Таск: Web `daemon` адаптер в `aoBridge`

**Тип:** frontend TS. **Оценка:** 1 чд. **Стори:** RW-2.1.

## Контекст
- `frontend/src/renderer/lib/bridge.ts` (351 LOC): фасада с browser stub, когда `window.ao` липсва. Стъбът на `daemon.getStatus()` връща `stopped`.
- `frontend/src/renderer/routes/_shell.tsx:557,1044,1088` изискват `daemonStatus.state === "ready" && port`.
- `frontend/src/renderer/lib/daemon-status.ts:6-13` `applyDaemonStatus`.
- Daemon health: `GET /healthz`, `GET /readyz` (router.go). През BFF това е same-origin `/readyz`.

## Събтаскове
- [ ] В `bridge.ts` browser stub: `daemon.getStatus()` прави `fetch(<base>/readyz)`; 200 → `{state:"ready", port: <от base URL или 0>}`; грешка → `{state:"error", message}`; `onStatus` polling на 5 s до ready, после на 30 s; `start/stop/restart` → no-op с `supported:false`.
- [ ] Увери се, че типът `DaemonStatus` допуска липсващ/нулев `port` в web режим или подай порта от `window.location.port`.
- [ ] Тестове (vitest, jsdom): ready при 200, error при network fail, polling спира при unmount.

## Критерии за приемане
- [ ] В web build без `VITE_NO_ELECTRON` `_shell` преминава gate-а срещу работещ daemon зад proxy.
