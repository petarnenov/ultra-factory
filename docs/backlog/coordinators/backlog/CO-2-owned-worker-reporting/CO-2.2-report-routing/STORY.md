# CO-2.2 · Story: Owner-routed reports and root summaries

**As** a coordinator **I want** reports from my workers delivered to me, and
**as** the root orchestrator **I want** concise coordinator summaries and
escalations **so that** I can coordinate the project without processing every
worker interaction.

**Estimate:** 2–3 pd. **Dependencies:** CO-2.1.1, CO-2.1.2.

| Task | Name | Estimate |
|---|---|---:|
| CO-2.2.1 | Route local and remote reports to worker owner | 1–2 pd |
| CO-2.2.2 | Deliver coordinator summaries and escalations to root | 1 pd |

**Acceptance criteria**

- [ ] Reports use the durable worker owner, including when pulled from a remote
  host.
- [ ] Legacy workers without an owner continue to route to the root.
- [ ] Failed delivery does not acknowledge or lose a report.
- [ ] Duplicate pulls do not create duplicate coordinator work.
- [ ] The root receives summaries and escalations without receiving every
  worker's raw report by default.
