# RW-5.4 · Story: Orchestrator in the hub
**Status:** ⬜ todo

**As** an operator **I want** the orchestrator to run in the hub, with its own credentials and wake-up loop, **so that** my Mac no longer has to stay on to coordinate the workers.

**Estimate:** 2–3 pd. **Dependencies:** RW-5.2, RW-5.3. #106 (report redelivery loop with TUI orchestrators) should be fixed first.

| Task | Name | pd |
|---|---|---|
| RW-5.4.1 | Agent credentials for orchestrators in the hub | 0.5 |
| RW-5.4.2 | Wake-up and reconcile loop inside the hub | 1 |
| RW-5.4.3 | Cutover from the Mac orchestrator and end-to-end check | 0.5–1 |

**Acceptance criteria**
- [ ] A hub orchestrator coordinates workers on IMac, Linux and Z600 end to end: spawn, reports, PR, merge into the integration branch.
- [ ] The Mac orchestrator and its launchd tick are retired, and no report from its last workers is lost.
