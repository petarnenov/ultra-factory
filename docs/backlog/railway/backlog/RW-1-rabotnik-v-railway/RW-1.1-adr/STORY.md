# RW-1.1 · Story: Architecture decisions (ADR)
**Status:** ✅ done (#60)

**As** a maintainer **I want** recorded decisions for the two deviations from the hard rules in `AGENTS.md`, **so that** the code in RW-1.3 and RW-2.4 can be reviewed against a clear policy.

**Estimate:** 1–2 pd. **Dependencies:** none. **Blocks:** RW-1.3, RW-2.4.

| Task | Name | pd |
|---|---|---|
| RW-1.1.1 | ADR: trusting a reverse proxy (X-Forwarded-For) for the remote-host listener | 0.5 |
| RW-1.1.2 | ADR: BFF in front of the daemon orchestrator in the hub | 0.5–1 |
| RW-1.1.3 | ADR: dual-stack bind of the remote-host listener | 0.5 |

**Acceptance criteria**
- [ ] Three ADRs in `docs/adr/` in the style of `0001`, `0003`, `0004`, with status "Accepted" or "Proposed" and a clear "Consequences" section.
- [ ] The `AGENTS.md` hard rules section links to the new ADRs where they extend it.
