# CO-3.1 · Story: Spawn coordinators and enforce capacity

**As** the root orchestrator **I want** to start two scoped coordinators and
give each up to three worker slots **so that** coordination work is divided
without exceeding the agreed six-worker limit.

**Estimate:** 1–2 pd. **Dependencies:** CO-2.1.2.

| Task | Name | Estimate |
|---|---|---:|
| CO-3.1.1 | Add coordinator spawn and capacity rules | 1–2 pd |

**Acceptance criteria**

- [ ] Only a project root can create coordinator sessions.
- [ ] Each coordinator has a non-empty scope and a maximum of three active
  owned workers.
- [ ] A project has at most two active coordinators and six active workers in
  this initial rollout.
- [ ] Failed spawns do not consume capacity; terminated workers release their
  slots according to the ADR.
- [ ] Existing direct root-owned worker spawning remains supported.
