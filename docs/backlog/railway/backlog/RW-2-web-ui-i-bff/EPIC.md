# RW-2 · Епик: Уеб UI и BFF в Railway

**Цел.** Renderer-ът на desktop приложението се билдва като статичен SPA и се сервира от hub service в Railway. BFF (Node) пред него прави login, proxy-ва REST, SSE и `/mux` към daemon-а на hub-а и към всеки регистриран работник с инжектиран Bearer. Работниците се добавят по адрес и парола (ръчно). Автоматичното създаване е RW-3.

**Оценка:** 11–18 чд. **Етап:** MVP-2. **Зависимости:** RW-1.1.2 (ADR за BFF), RW-1.4 (валидиран работник).

| Стори | Име | чд |
|---|---|---|
| RW-2.1 | Web build на renderer-а: base URL и readiness | 2–3 |
| RW-2.2 | Скриване на Electron-only функции в web build | 1.5–2 |
| RW-2.3 | Web адаптери: shortcuts, notifications, settings, clipboard | 1.5–3 |
| RW-2.4 | BFF: login, proxy, registry на хостове | 5–8 |
| RW-2.5 | Hub image и деплой | 1–2 |

**Критерии за приемане на епика**
- [ ] От браузър през `https://hub.<domain>`: login, board, стартиране на сесия на работник, чат, терминал, live updates, PR статус.
- [ ] Няма `127.0.0.1` допускания в web build-а; CSP позволява само same-origin.
- [ ] Electron-only функции са скрити, не чупят екрана.
- [ ] `npm run typecheck`, `npm run test` (frontend) и `make check` са зелени; Electron build-ът е без регресии.

**Какво не влиза.** Вграден browser panel, агентски CDP браузър, анотации, импорт на браузърни профили, editor handoff, auto-update, AO Cloud login.
