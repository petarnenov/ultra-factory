# RW-1 · Epic: Worker on Railway, managed from the desktop app
**Status:** ⛔ needs-human

**Goal.** A single `ao daemon` runs as a Railway service with a persistent volume and the authenticated remote-host listener enabled. The desktop app adds it in Settings → Remote hosts and starts a session on it. This validates Railway networking, the image, and agent authentication before investing in a web UI.

**Outcome.** A published worker image, an entrypoint, ADRs for the decisions, and a "how to deploy a worker manually" document.

**Estimate:** 6–9 pd. **Stage:** MVP-1.

| Story | Name | pd |
|---|---|---|
| RW-1.1 | Architecture decisions (ADR) | 1–2 |
| RW-1.2 | Worker image and entrypoint | 2–3 |
| RW-1.3 | Remote-host listener behind a reverse proxy | 2–3 |
| RW-1.4 | Manual deploy and connection from the desktop app | 1 |

**Epic acceptance criteria**
- [ ] The desktop app shows a worker on Railway; a Claude Code session starts, the terminal works, and a PR gets created.
- [ ] After `railway redeploy`, the DB, worktrees, and agent credentials are preserved.
- [ ] The main API (:3001) is not reachable from outside. The only public surface is :3011 with Bearer.

**Risks.** IPv6-only private networking; image size; OAuth login of the agents inside a container.
