# RW-2.4.4 · Таск: Renderer multi-host през BFF вместо Electron `remotes`

**Тип:** frontend TS. **Оценка:** 0.5–1 чд. **Стори:** RW-2.4. **Зависимости:** RW-2.4.3, RW-2.2.1.

## Контекст
- `aoBridge.remotes` (11 метода, 30 реф.) днес вика Electron main (`remotes-store.ts`, `remote-proxy.ts`) и връща loopback proxy URL за всеки хост. `lib/host-clients.ts` `clientForHost(hostId)` строи `apiClient` по този URL.
- Маршрути `host.$hostId.*` и `RemoteHostsSettings.tsx`.

## Събтаскове
- [ ] Web stub за `aoBridge.remotes`: `list` → `GET /hub/hosts`; `add` → `POST /hub/hosts`; `remove` → `DELETE`; `probe` → `POST .../probe`; `baseUrlFor(hostId)` → `"/hosts/<id>"` (same-origin префикс).
- [ ] `host-clients.ts`: base URL за хост в web режим е префикс, не абсолютен URL; `muxUrlFromApiBase` и SSE конструкцията работят с префикс (провери `terminal-mux.ts:72-77`).
- [ ] `RemoteHostsSettings.tsx`: същият UI, данните идват от stub-а; при web скрий полетата, които нямат смисъл (tunnel, LAN адреси).
- [ ] Тестове на stub-а и на `host-clients` с префикс.

## Критерии за приемане
- [ ] В web build: Settings → Remote hosts показва работника, `host/<id>` маршрутът отваря board-а му, терминал работи.
