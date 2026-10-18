# CO-2 · Epic: Persist coordinator ownership and route reports

**Status:** ⬜ todo

**Goal.** Store each worker's coordinator owner and deliver worker reports to
that owner, while preserving the root orchestrator as the project-level
coordinator and fallback for legacy workers.

**Estimate:** 4–6 pd. **Stage:** core. **Dependencies:** CO-1.2.1.

| Story | Name | Estimate |
|---|---|---:|
| CO-2.1 | Durable coordinator and worker ownership | 2–3 pd |
| CO-2.2 | Owner-routed reports and root summaries | 2–3 pd |

**Acceptance criteria**

- [ ] Worker ownership survives daemon restart and is visible through the
  daemon API.
- [ ] Local and remote worker reports reach their assigned coordinator.
- [ ] Legacy links without coordinator ownership continue to reach the root.
- [ ] Retries, acknowledgements, and duplicate protection remain correct.
- [ ] A coordinator can send a concise summary or escalation to the root.
