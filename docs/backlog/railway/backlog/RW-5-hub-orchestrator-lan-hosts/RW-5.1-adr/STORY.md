# RW-5.1 · Story: ADR for the network path from the hub to machines outside Railway
**Status:** ✅ done

**As** a maintainer **I want** a recorded decision on how the Railway hub reaches machines on a private network, and how a saved bearer may cross that path, **so that** RW-5.2 and RW-5.3 can be reviewed against a clear policy.

**Estimate:** 0.5–1 pd. **Dependencies:** RW-4.1.1 (threat model). **Blocks:** RW-5.2, RW-5.3.

| Task | Name | pd |
|---|---|---|
| RW-5.1.1 | ADR: hub egress to off-Railway hosts (Tailscale userspace vs Cloudflare Tunnel) | 0.5–1 |

**Acceptance criteria**
- [ ] `docs/adr/0008-…` with status "Proposed", merged only after user approval (`needs-human`).
- [ ] The ADR states the exception to `DirectClient`'s "no proxy" rule precisely: which proxy, which destinations, and why the bearer stays confidential.
