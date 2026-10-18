# CO-1.1 · Story: Baseline resource use under six workers

**As** an operator **I want** per-process resource measurements during a
six-worker run **so that** I can tell whether coordination work is a material
cause of the hub's memory peak.

**Estimate:** 0.5–1 pd. **Dependencies:** none.

| Task | Name | Estimate |
|---|---|---:|
| CO-1.1.1 | Record per-process resource baseline | 0.5–1 pd |

**Acceptance criteria**

- [ ] A repeatable six-worker workload and observation window are documented.
- [ ] Peak and average CPU/RSS are captured for daemon, BFF, orchestrator,
  workers, and notable child processes where the platform exposes them.
- [ ] Samples include timestamps and are compared with Railway service-level
  CPU and memory metrics.
- [ ] The result states measurement gaps and does not attribute aggregate
  Railway metrics to a process without evidence.
