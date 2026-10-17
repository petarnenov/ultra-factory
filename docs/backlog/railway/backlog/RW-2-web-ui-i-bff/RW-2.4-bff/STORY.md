# RW-2.4 · Story: BFF for the hub (login, proxy, host registry)
**Status:** ✅ done

**As** an operator **I want** a single public HTTPS endpoint with login that serves the SPA and proxies REST/SSE/WebSocket to the hub's daemon and to every registered worker with an injected Bearer, **so that** the web UI works without exposing the daemon API.

**Estimate:** 5–8 pd. **Dependencies:** RW-1.1.2 (ADR), RW-2.1. **Blocks:** RW-2.5, RW-3.

| Task | Name | pd |
|---|---|---|
| RW-2.4.1 | BFF skeleton: static SPA, login, session cookie, CSRF | 1.5–2 |
| RW-2.4.2 | Reverse proxy for REST/SSE/`/mux` to the hub daemon and to hosts | 2–3 |
| RW-2.4.3 | Host registry (`hosts.json`) and API for adding/removing by address | 1–2 |
| RW-2.4.4 | Renderer: multi-host routes through the BFF instead of Electron `remotes` | 0.5–1 |

**Acceptance criteria**
- [ ] Without a cookie: everything except `/login` and `/healthz` → 401.
- [ ] With a cookie: `/api/*`, `/mux`, SSE to the hub work; `/hosts/<id>/api/*`, `/hosts/<id>/mux` to the worker work with a Bearer that never reaches the browser.
- [ ] The operator password and host credentials live only on the volume (0600) or in Railway variables.
