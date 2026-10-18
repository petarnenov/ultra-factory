# CO-2.1 · Story: Durable coordinator and worker ownership

**As** the daemon **I want** to persist coordinator ownership for each worker
**so that** ownership is stable across restarts and can route reports.

**Estimate:** 2–3 pd. **Dependencies:** CO-1.2.1.

| Task | Name | Estimate |
|---|---|---:|
| CO-2.1.1 | Persist coordinator ownership | 1–2 pd |
| CO-2.1.2 | Expose ownership through daemon and CLI APIs | 1 pd |

**Acceptance criteria**

- [ ] Ownership is durable, validated, and tied to sessions in the same
  project.
- [ ] Ownership can be queried and changed only through authorized daemon
  service boundaries.
- [ ] Existing sessions without an owner remain valid and resolve to the root
  using the compatibility rule in the ADR.
- [ ] Storage migration and generated artifacts follow `AGENTS.md`.
