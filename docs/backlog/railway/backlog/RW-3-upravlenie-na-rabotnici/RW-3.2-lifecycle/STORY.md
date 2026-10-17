# RW-3.2 · Story: Worker lifecycle
**Status:** ✅ done

**As** an operator **I want** the BFF to take the worker through the states provisioning → online → removing → removed with visible progress and recovery after a hub restart, **so that** no "orphans" are left behind in Railway.

**Estimate:** 2–3 pd. **Dependencies:** RW-3.1, RW-2.4.3.

| Task | Name | pd |
|---|---|---|
| RW-3.2.1 | State machine + persisted jobs in `hosts.json` | 1–1.5 |
| RW-3.2.2 | Reconcile on startup and periodically; protection against orphans and a worker count limit | 1–1.5 |

**Acceptance criteria**
- [ ] Killing the hub in the middle of provisioning → after restart the worker either completes or is marked failed with a reason; a service is never left without a record.
- [ ] `AO_HUB_MAX_WORKERS` is enforced with a clear error in the UI.
