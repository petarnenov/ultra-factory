# RW-2.1 · Story: Web build of the renderer with a configurable base URL

**As** a browser user **I want** the UI to talk to the daemon same-origin (`/api`, `/mux`) and to pass the readiness gate without Electron, **so that** the real board loads, not mock data.

**Estimate:** 2–3 pd. **Dependencies:** none. **Blocks:** RW-2.2–2.5.

| Task | Name | pd |
|---|---|---|
| RW-2.1.1 | Web `daemon` adapter in `aoBridge` (health probe → ready) | 1 |
| RW-2.1.2 | Base URL without `127.0.0.1` and CSP for the web build | 0.5–1 |
| RW-2.1.3 | Vite web build target (`npm run build:web`) without `VITE_NO_ELECTRON` | 0.5–1 |

**Acceptance criteria**
- [ ] `npm run build:web` produces `dist-web/` with static files and hash history.
- [ ] Served behind a proxy to a real daemon, the board shows real projects/sessions; SSE and the terminal work.
- [ ] The Electron build does not change its behavior (`applyDaemonStatus` in Electron stays `127.0.0.1:port`).
