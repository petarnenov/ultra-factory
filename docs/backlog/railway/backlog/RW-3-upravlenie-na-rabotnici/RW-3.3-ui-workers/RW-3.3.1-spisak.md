# RW-3.3.1 · Task: Worker list

**Type:** frontend React. **Estimate:** 0.5–1 pd. **Story:** RW-3.3.

## Subtasks
- [ ] `components/settings/WorkersSettings.tsx`: table/cards with label, status badge (provisioning with a spinner, online, unreachable, auth_failed, removing, failed with a `lastError` tooltip), number of active sessions, created at, source (manual/railway), link to `host/<id>`.
- [ ] Orphans section (from RW-3.2.2) with "Adopt" and "Delete".
- [ ] Query hook `useHubHosts()` with adaptive polling.
- [ ] Vitest: rendering of all statuses.
