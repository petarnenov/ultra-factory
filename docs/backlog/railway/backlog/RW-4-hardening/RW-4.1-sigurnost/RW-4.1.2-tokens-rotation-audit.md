# RW-4.1.2 · Task: Railway token scope, rotation, audit log
**Status:** ✅ done (#107)

**Type:** operations + Node TS. **Estimate:** 0.5–1 pd. **Story:** RW-4.1.

## Subtasks
- [ ] Use a project-scoped Railway token (from RW-3.1.1), document the minimum permissions; refuse to start if the token is account-wide (if the API makes it possible to tell the difference).
- [ ] Rotation: runbook for the operator password, session secret, Railway token, worker passwords (`ao remote-host` regenerate via `/hosts/:id/api/v1/...` if there is a route, otherwise redeploy with a new `AO_REMOTE_PASSWORD` and update in the registry).
- [ ] Audit log (append-only JSONL on the volume): login ok/fail, host add/remove, worker create/delete, force kill. No secrets.
- [ ] `GET /hub/audit?limit=` for the UI (optional).
