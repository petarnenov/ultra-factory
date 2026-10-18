# CO-3.2 · Story: Coordinate worker groups and return summaries

**As** a project root **I want** each helper to manage a bounded work area and
report progress, results, and blockers **so that** I can focus on cross-area
decisions and final integration.

**Estimate:** 1–2 pd. **Dependencies:** CO-2.2.2, CO-3.1.1.

| Task | Name | Estimate |
|---|---|---:|
| CO-3.2.1 | Define coordinator operating prompt and handoff | 1–2 pd |

**Acceptance criteria**

- [ ] Coordinator instructions assign a distinct work scope and prohibit
  cross-scope edits unless the root approves.
- [ ] Coordinators report concise status, outcomes, and escalations to root.
- [ ] Coordinators do not perform final integration or merge.
- [ ] The root can pause, resume, replace, or reassign a coordinator's work
  without losing worker ownership or reports.
