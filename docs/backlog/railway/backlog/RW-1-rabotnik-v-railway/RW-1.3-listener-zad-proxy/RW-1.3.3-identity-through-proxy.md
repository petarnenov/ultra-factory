# RW-1.3.3 · Task: Identity probe and Expected-Host-ID guard through a proxy
**Status:** ✅ done (#71)

**Type:** backend Go + verification. **Estimate:** 0.5 pd. **Story:** RW-1.3.

## Goal
Confirm that `GET /api/v1/identity` (the only unauthenticated route) and `X-AO-Expected-Host-ID` (421 on mismatch, `lan_listener.go:138`) work correctly through an HTTPS proxy, and that the desktop/CLI clients use them when adding a host.

## Context
- `backend/internal/httpd/auth.go:245-252` identity exemption; ADR `0003`.
- `frontend/src/main/remote-request.ts:45-80` probe; `backend/internal/cli/remote_target.go:48-51` `remoteIdentity`.

## Subtasks
- [ ] Verify that the identity response includes nothing sensitive when served on a public URL (only `hostId`, `apiVersion`).
- [ ] Verify that `expectedHostGuard` does not depend on the `Host` header (which the proxy may rewrite).
- [ ] If an `https://` URL to a host is rejected anywhere in the client (for example by a scheme allowlist), fix it. Look for `http://` assumptions in `remote-request.ts`, `remote_target.go`.
- [ ] Add a test in `remote_target_test.go` for an `https` URL.

## Acceptance criteria
- [ ] `ao host ls` after adding `https://worker.up.railway.app` shows the hostId; `ao project ls --host worker` works.
