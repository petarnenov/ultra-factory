# CO-4.2 · Story: Roll out and recover the coordinator hierarchy

**As** an operator **I want** documented enablement, monitoring, and rollback
steps **so that** the coordinator topology can be introduced gradually and
recovered without losing work.

**Estimate:** 1–2 pd. **Dependencies:** CO-4.1.1.

| Task | Name | Estimate |
|---|---|---:|
| CO-4.2.1 | Document coordinator rollout and rollback | 1–2 pd |

**Acceptance criteria**

- [ ] Rollout is opt-in and starts with a pilot project.
- [ ] Operators can disable new coordinator creation while existing workers
  continue to report.
- [ ] Rollback returns workers to root ownership without dropping pending
  reports.
- [ ] Monitoring includes capacity, stuck reports, coordinator liveness, and
  per-process memory.
