# RW-2.5 · Story: Hub image and deployment

**As** an operator **I want** a single Railway service `hub` with the daemon orchestrator, BFF and SPA, **so that** I have a public web UI.

**Estimate:** 1–2 pd. **Dependencies:** RW-1.2 (base), RW-2.1.3, RW-2.4.

| Task | Name | pd |
|---|---|---|
| RW-2.5.1 | Hub Dockerfile (worker image + BFF + SPA) and a supervisor for two processes | 1 |
| RW-2.5.2 | Hub deployment on Railway, domain, env, runbook | 0.5–1 |

**Acceptance criteria**
- [ ] `https://hub.<domain>` → login → the board of the hub and of the worker from RW-1.4.
- [ ] The processes restart independently; SIGTERM stops both cleanly.
