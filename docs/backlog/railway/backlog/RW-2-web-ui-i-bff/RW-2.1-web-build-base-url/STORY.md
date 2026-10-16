# RW-2.1 · Стори: Web build на renderer-а с конфигурируем base URL

**Като** потребител в браузър **искам** UI-ят да говори с daemon-а на same-origin (`/api`, `/mux`) и да преминава readiness gate-а без Electron, **за да** се зареди реалният board, не mock данни.

**Оценка:** 2–3 чд. **Зависимости:** няма. **Блокира:** RW-2.2–2.5.

| Таск | Име | чд |
|---|---|---|
| RW-2.1.1 | Web `daemon` адаптер в `aoBridge` (health probe → ready) | 1 |
| RW-2.1.2 | Base URL без `127.0.0.1` и CSP за web build | 0.5–1 |
| RW-2.1.3 | Vite web build target (`npm run build:web`) без `VITE_NO_ELECTRON` | 0.5–1 |

**Критерии за приемане**
- [ ] `npm run build:web` произвежда `dist-web/` със статични файлове и hash history.
- [ ] Сервиран зад proxy към истински daemon, board-ът показва реални проекти/сесии, SSE и терминал работят.
- [ ] Electron build-ът не променя поведението си (`applyDaemonStatus` в Electron остава `127.0.0.1:port`).
