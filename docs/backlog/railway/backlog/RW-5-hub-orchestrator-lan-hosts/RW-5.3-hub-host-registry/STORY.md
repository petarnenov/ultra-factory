# RW-5.3 · Story: One host list for the hub UI and `ao spawn --host`
**Status:** 🔄 in progress

**As** an operator **I want** a machine I add in the hub UI to be usable by the orchestrator running in the hub, **so that** I manage the list of machines in one place.

**Estimate:** 1 pd. **Dependencies:** RW-5.1. **Blocks:** RW-5.4.3.

| Task | Name | pd |
|---|---|---|
| RW-5.3.1 | Mirror the hub host registry into the daemon's `remotes.json` | 1 |

**Acceptance criteria**
- [ ] After adding a host in the hub UI, `ao host ls` inside the hub lists it as `online`. After removing it, the host disappears from both lists.
