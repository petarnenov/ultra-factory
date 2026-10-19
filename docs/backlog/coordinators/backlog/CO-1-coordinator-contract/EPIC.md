# CO-1 · Epic: Measure load and define coordinator ownership

**Status:** 🔄 in progress — CO-1.2.1 merged in [#282](https://github.com/petarnenov/ultra-factory/pull/282); CO-1.1.1 still needs an operator run ([#218](https://github.com/petarnenov/ultra-factory/issues/218)).

**Goal.** Establish a measured baseline and an explicit domain/API contract for
one root orchestrator delegating to two scoped coordinators, each managing up
to three workers.

**Estimate:** 2–3 pd. **Stage:** design gate. **Dependencies:** none.

| Story | Name | Estimate |
|---|---|---:|
| CO-1.1 | Baseline resource use under six workers | 0.5–1 pd |
| CO-1.2 | Coordinator ownership ADR and contract | 1–2 pd |

**Acceptance criteria**

- [ ] The six-worker workload records memory and CPU by process, with
  timestamps that can be compared to Railway service metrics.
- [ ] An ADR defines root, coordinator, worker, ownership, reporting,
  reassignment, and failure semantics.
- [ ] The ADR confirms the one-root-per-project invariant and rejects
  replicated hub services with a shared SQLite volume as the coordinator
  strategy.
- [ ] The report route and persistence changes needed for the pilot are
  identified before implementation stories proceed.
