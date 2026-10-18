# CO-4.1 · Story: End-to-end pilot and resource comparison

**As** an operator **I want** to exercise the full coordinator hierarchy under
the six-worker load **so that** I can verify correctness and measure whether
the root's coordination load improves.

**Estimate:** 1–2 pd. **Dependencies:** CO-3.1.1, CO-3.2.1, CO-3.3.1.

| Task | Name | Estimate |
|---|---|---:|
| CO-4.1.1 | Run coordinator hierarchy end-to-end | 1–2 pd |

**Acceptance criteria**

- [ ] Two coordinators each manage three workers through assignment, report,
  summary, and completion.
- [ ] Duplicate report delivery, coordinator restart, failed worker, and
  reassignment cases are verified.
- [ ] Root-only merge authority is verified.
- [ ] Resource results are compared to CO-1.1.1 using equivalent workloads.
- [ ] Any increase in hub load or unresolved failure mode is recorded before
  rollout.
