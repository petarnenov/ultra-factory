# RW-2 · Epic: Web UI and BFF on Railway
**Status:** ✅ done

**Goal.** The desktop app's renderer is built as a static SPA and served by a hub service on Railway. A BFF (Node) in front of it handles login and proxies REST, SSE and `/mux` to the hub's daemon and to every registered worker, with an injected Bearer. Workers are added by address and password (manually). Automatic creation is RW-3.

**Estimate:** 11–18 pd. **Stage:** MVP-2. **Dependencies:** RW-1.1.2 (ADR for the BFF), RW-1.4 (validated worker).

| Story | Name | pd |
|---|---|---|
| RW-2.1 | Web build of the renderer: base URL and readiness | 2–3 |
| RW-2.2 | Hiding Electron-only features in the web build | 1.5–2 |
| RW-2.3 | Web adapters: shortcuts, notifications, settings, clipboard | 1.5–3 |
| RW-2.4 | BFF: login, proxy, host registry | 5–8 |
| RW-2.5 | Hub image and deployment | 1–2 |

**Epic acceptance criteria**
- [ ] From a browser via `https://hub.<domain>`: login, board, starting a session on a worker, chat, terminal, live updates, PR status.
- [ ] No `127.0.0.1` assumptions in the web build; CSP allows same-origin only.
- [ ] Electron-only features are hidden and do not break the screen.
- [ ] `npm run typecheck`, `npm run test` (frontend) and `make check` are green; the Electron build has no regressions.

**Out of scope.** Embedded browser panel, agent CDP browser, annotations, browser profile import, editor handoff, auto-update, AO Cloud login.
