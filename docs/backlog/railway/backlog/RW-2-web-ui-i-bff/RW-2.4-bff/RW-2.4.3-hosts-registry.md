# RW-2.4.3 · Task: Host registry and API for adding/removing by address

**Type:** Node TS. **Estimate:** 1–2 pd. **Story:** RW-2.4. **Dependencies:** RW-2.4.1.

## Context
- Format compatible with the desktop `~/.ao/remotes.json` and the CLI `remoteEntry` (`backend/internal/cli/remote_target.go:31-37`): `{label, url, password, hostId, accountUserId?}`. Extend it with `source: "manual"|"railway"`, `railway?: {serviceId, volumeId, environmentId}`, `createdAt`, `status`.
- Path: `AO_HUB_DATA_DIR/hosts.json` on the hub's volume, 0600, atomic write (tmp + rename).
- Identity probe: `GET <url>/api/v1/identity` → `{hostId, apiVersion}` (unauthenticated); then `GET <url>/api/v1/projects` with a Bearer to validate the password.

## Subtasks
- [ ] `hosts-store.ts`: load/save/list/get/add/remove with validation (`url` only `http(s)://`, label unique).
- [ ] BFF API (auth required): `GET /hub/hosts` (without passwords), `POST /hub/hosts` `{label,url,password}` → probe identity + auth check → write; `DELETE /hub/hosts/:id` (does not delete anything in Railway); `POST /hub/hosts/:id/probe` → status.
- [ ] Status polling: every 30 s probe every host, `status: online|unreachable|auth_failed`, exposed in `GET /hub/hosts`.
- [ ] The `hub` entry is created automatically at startup from `mobile/config.json` on the same volume (B2) and cannot be deleted.
- [ ] Tests with a fake host.

## Acceptance criteria
- [ ] Adding the validated worker from RW-1.4 via `curl` to the BFF, after which `/hosts/<id>/api/v1/projects` works.
