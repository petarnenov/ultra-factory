# CO-1.2 · Story: Coordinator ownership contract

**As** an AO maintainer **I want** durable, unambiguous coordinator ownership
semantics **so that** workers and reports can be assigned without creating
competing project orchestrators.

**Estimate:** 1–2 pd. **Dependencies:** CO-1.1.1.

| Task | Name | Estimate |
|---|---|---:|
| CO-1.2.1 | ADR for coordinator hierarchy and report routing | 1–2 pd |

**Acceptance criteria**

- [ ] The contract defines a single root and scoped coordinator sessions.
- [ ] It defines report delivery, summaries, reassignments, restart behavior,
  and coordinator failure recovery.
- [ ] It specifies the limits of two coordinators, three active workers each,
  and six workers total for the initial rollout.
- [ ] The ADR identifies compatibility behavior for existing remote-worker
  links and direct root-owned workers.
