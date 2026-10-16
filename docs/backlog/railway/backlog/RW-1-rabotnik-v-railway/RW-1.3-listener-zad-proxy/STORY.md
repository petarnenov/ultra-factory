# RW-1.3 · Story: Remote-host listener behind a reverse proxy

**As** an operator **I want** the remote-host listener to work correctly behind the Railway edge and over IPv6 private networking, **so that** a single failed attempt does not lock out all clients and the listener is reachable from the hub.

**Estimate:** 2–3 pd. **Dependencies:** RW-1.1.1, RW-1.1.3. **Blocks:** RW-1.4 (partially), RW-3.

| Task | Name | pd |
|---|---|---|
| RW-1.3.1 | Trusted-proxy mode for lockout | 1 |
| RW-1.3.2 | Dual-stack bind of the listener | 0.5–1 |
| RW-1.3.3 | Expected-host-ID guard and identity probe through a proxy | 0.5 |

**Acceptance criteria**
- [ ] 5 wrong passwords from client A do not lock out client B behind the same proxy when trusted-proxy mode is on.
- [ ] `curl -6 http://[<v6>]:3011/api/v1/identity` → 200 with dual-stack enabled.
- [ ] Behavior without the env variables is identical to today's (LAN users).
