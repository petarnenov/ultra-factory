# RW-2.4 · Стори: BFF за hub-а (login, proxy, registry на хостове)

**Като** оператор **искам** един публичен HTTPS endpoint с login, който сервира SPA-то и proxy-ва REST/SSE/WebSocket към daemon-а на hub-а и към всеки регистриран работник с инжектиран Bearer, **за да** работи уеб UI-ят без да се излага daemon API-то.

**Оценка:** 5–8 чд. **Зависимости:** RW-1.1.2 (ADR), RW-2.1. **Блокира:** RW-2.5, RW-3.

| Таск | Име | чд |
|---|---|---|
| RW-2.4.1 | Скелет на BFF: static SPA, login, session cookie, CSRF | 1.5–2 |
| RW-2.4.2 | Reverse proxy за REST/SSE/`/mux` към hub daemon-а и към хостове | 2–3 |
| RW-2.4.3 | Registry на хостове (`hosts.json`) и API за добавяне/махане по адрес | 1–2 |
| RW-2.4.4 | Renderer: multi-host маршрути през BFF вместо Electron `remotes` | 0.5–1 |

**Критерии за приемане**
- [ ] Без cookie: всичко освен `/login` и `/healthz` → 401.
- [ ] С cookie: `/api/*`, `/mux`, SSE към hub-а работят; `/hosts/<id>/api/*`, `/hosts/<id>/mux` към работника работят с Bearer, който никога не стига до браузъра.
- [ ] Паролата на оператора и host credentials са само на volume (0600) или в Railway variables.
