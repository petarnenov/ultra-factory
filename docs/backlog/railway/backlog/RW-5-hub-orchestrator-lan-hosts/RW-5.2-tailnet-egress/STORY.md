# RW-5.2 · Story: Tailnet egress from the hub
**Status:** ⬜ todo

**As** an operator **I want** the hub's daemon and BFF to reach my machines over my tailnet, **so that** the hub can manage them without any port open at home.

**Estimate:** 3–4 pd. **Dependencies:** RW-5.1 (ADR accepted with option T1). **Blocks:** RW-5.4.3.

If the ADR picks T2 or T3 instead, this story shrinks to a runbook task: run `cloudflared` or Funnel on each host and configure trusted-proxy mode. The daemon and BFF tasks are then dropped.

| Task | Name | pd |
|---|---|---|
| RW-5.2.1 | Tailscale userspace in the hub image and supervisor | 1 |
| RW-5.2.2 | Daemon: explicit tailnet dialer for remote hosts | 1–1.5 |
| RW-5.2.3 | BFF: tailnet dialer for probe, workers API and proxy (REST, SSE, `/mux`) | 1–1.5 |

**Acceptance criteria**
- [ ] With `TS_AUTHKEY` set, the hub reaches `http://<tailnet-ip>:<port>/api/v1/identity` of a LAN machine from both the daemon and the BFF.
- [ ] Without `TS_AUTHKEY` the hub behaves exactly as before, and all existing tests pass.
