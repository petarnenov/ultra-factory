# RW-2.1.2 · Таск: Base URL без `127.0.0.1` и CSP за web build

**Тип:** frontend TS + Vite. **Оценка:** 0.5–1 чд. **Стори:** RW-2.1.

## Контекст
- `frontend/src/renderer/lib/api-client.ts:12-26`: `VITE_AO_API_BASE_URL` има приоритет; празен string = same-origin; `muxUrlFromApiBase("")` ползва `window.location` (`terminal-mux.ts:72-77`).
- `frontend/src/renderer/lib/daemon-status.ts:8-11`: натрапва `http://127.0.0.1:${port}` при ready.
- `frontend/vite.renderer.csp.ts:80-86`: `connect-src` закован на `http://127.0.0.1:*`, `ws://127.0.0.1:*`.
- SSE sites: `event-transport.ts:437,467`, `notifications.ts:650`, `workspace-file-events.ts:202`, `chat/IndependentSideChats.tsx:513`, `remote-sse-probe.ts:33`. Всички ползват base URL от api-client.

## Събтаскове
- [ ] `applyDaemonStatus`: ако `import.meta.env.VITE_AO_WEB === "1"` (нов флаг за web target), при ready сетни base URL на `""` (same-origin) или на `VITE_AO_API_BASE_URL`; иначе текущото поведение.
- [ ] Провери петте SSE места и `/mux`, че конструират URL от `getApiBaseUrl()`, не от hardcoded host. Поправи отклоненията.
- [ ] `vite.renderer.csp.ts`: при `VITE_AO_WEB` генерирай `connect-src 'self' wss: https:` ограничено до same-origin (`'self'` покрива ws/wss same-origin в модерни браузъри; добави изрично `wss://<host>` ако е нужно). `img-src` без `127.0.0.1`.
- [ ] Тестове: `api-client.test.ts` за web режим; CSP unit тест (`vite.renderer.config.test.ts` стил).

## Критерии за приемане
- [ ] В web build Network tab показва само same-origin заявки; няма CSP грешки в конзолата.
