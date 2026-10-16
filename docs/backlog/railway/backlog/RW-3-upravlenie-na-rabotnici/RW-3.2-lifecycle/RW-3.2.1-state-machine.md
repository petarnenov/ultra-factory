# RW-3.2.1 · Task: State machine and persisted jobs

**Type:** Node TS. **Estimate:** 1–1.5 pd. **Story:** RW-3.2.

## Subtasks
- [ ] Extend the host record: `status: provisioning|online|unreachable|auth_failed|removing|failed`, `railway: {serviceId, volumeId, environmentId}`, `lastError`, `desired: running|removed` (modeled on `desired_state` in the cloud reconciler).
- [ ] `POST /hub/workers` `{label, size?, env: {ANTHROPIC_API_KEY?, CLAUDE_CODE_OAUTH_TOKEN?, GH_TOKEN, AO_GIT_USER_NAME, AO_GIT_USER_EMAIL}}`: generates `AO_REMOTE_PASSWORD` (32 characters, not 8), writes the host with status provisioning **before** calling Railway (write-ahead), then `provider.create`, writes the ids.
- [ ] Poll: status running → probe `GET <url>/api/v1/identity` until hostId → writes hostId, status online. Timeout 10 min → failed + `lastError`; the service is kept for diagnostics (not deleted automatically).
- [ ] `DELETE /hub/workers/:id`: if there are active sessions (`GET /hosts/:id/api/v1/sessions` filtered by running) → 409 with the list, unless `?force=1`; otherwise status removing → `provider.delete` → removal from the registry.
- [ ] Worker URL: `http://<service-name>.railway.internal:3011` if RW-1.4.1 confirms private networking; otherwise a public domain created by the provider.
- [ ] Tests with a fake provider: happy path, Railway error on create, timeout, delete with active sessions.

## Acceptance criteria
- [ ] All transitions are covered by tests; `hosts.json` never loses a record on a crash (write-ahead + atomic rename).
