# CO-3.3 · Story: Inspect coordinator hierarchy

**As** an operator **I want** to see coordinators, their scopes, workers, and
current states **so that** I can tell where work is assigned and recover it
when a coordinator stalls.

**Estimate:** 1 pd. **Dependencies:** CO-2.1.2, CO-2.2.2.

| Task | Name | Estimate |
|---|---|---:|
| CO-3.3.1 | Show coordinator ownership in session listings | 1 pd |

**Acceptance criteria**

- [ ] The operator can identify the root, each coordinator, and each owned
  worker from supported CLI/API views.
- [ ] Views show scope, active/terminated state, and latest summary or blocker.
- [ ] Existing session listing behavior remains usable for projects without
  coordinators.
