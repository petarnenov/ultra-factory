# RW-3 · Epic: Adding and removing workers from the web UI

**Goal.** From Settings → Workers in the web UI, the operator clicks "Add worker", sets a name, keys and size, and the BFF creates a Railway service with a volume from the worker image, waits for it to come online and registers it. "Remove" deletes the service and the volume after confirmation.

**Estimate:** 5–8 pd. **Stage:** full goal. **Dependencies:** RW-1 (image, validated networking), RW-2.4 (BFF, registry).

| Story | Name | pd |
|---|---|---|
| RW-3.1 | Railway API client in the BFF | 1.5–2 |
| RW-3.2 | Worker lifecycle: provisioning → online → removing | 2–3 |
| RW-3.3 | UI: Workers screen with add and remove | 1.5–3 |

**Epic acceptance criteria**
- [ ] Adding a worker from the UI takes < 5 min to reach online status with no manual steps in Railway.
- [ ] Removal shows the active sessions, requires confirmation by name, deletes service + volume, removes the record.
- [ ] The Railway token never leaves the BFF; the worker count limit is configurable (`AO_HUB_MAX_WORKERS`, default 5).

**Open questions (resolved in RW-3.1.1).** The exact GraphQL mutations of the Railway public API for a service from an image + volume + variables + domain; project-scoped vs account token; behavior when the service name is taken.
