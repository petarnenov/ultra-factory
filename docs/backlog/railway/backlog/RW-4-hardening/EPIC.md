# RW-4 · Epic: Hardening, tests, documentation
**Status:** ⬜ todo

**Goal.** The system is safe for a public URL, costs are capped, there is an e2e test and operator documentation.

**Estimate:** 3–5 pd. **Stage:** full goal. **Dependencies:** RW-2, RW-3.

| Story | Name | pd |
|---|---|---|
| RW-4.1 | Security: secrets, token scope, headers, audit | 1–2 |
| RW-4.2 | Reliability: backup, redeploy strategy, limits | 1 |
| RW-4.3 | E2E test (Playwright web) and documentation | 1–2 |

**Acceptance criteria**
- [ ] Threat model document with mitigations for: stolen cookie, compromised worker, leaked Railway token, brute force.
- [ ] Playwright e2e passes against a local docker-compose of hub + worker.
- [ ] `docs/` describes the architecture and operations; `AGENTS.md` links to it.
