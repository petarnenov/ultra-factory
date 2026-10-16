# RW-3.2.2 · Task: Reconcile, orphans and limits

**Type:** Node TS. **Estimate:** 1–1.5 pd. **Story:** RW-3.2. **Dependencies:** RW-3.2.1.

## Subtasks
- [ ] On BFF startup: for every host with `status ∈ {provisioning, removing}`, resume the job (poll or delete).
- [ ] Periodically (5 min): `provider.list()` of services with label/tag `ao-worker=1` in the project; a service with no record in `hosts.json` → WARNING log "orphan" + shown in the UI with an "Adopt"/"Delete" button. Never delete automatically.
- [ ] `AO_HUB_MAX_WORKERS` (default 5): `POST /hub/workers` → 429 when reached, with the count and the limit in the body.
- [ ] Metrics in the log: number of workers by status, last reconcile.
- [ ] Tests: orphan detection, resume after restart, limit.
