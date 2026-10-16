# RW-1.3.1 · Task: Trusted-proxy mode for lockout

**Type:** backend Go, security. **Estimate:** 1 pd. **Story:** RW-1.3. **Dependencies:** RW-1.1.1.

## Goal
With `AO_REMOTE_TRUSTED_PROXY=1`, `authMiddleware` uses the client IP from `X-Forwarded-For` (the first element) as the lockout key instead of `RemoteAddr`.

## Context
- `backend/internal/httpd/auth.go:144` `sourceKey`; `auth.go:260-270` CF-Connecting-IP logic for a loopback peer. Extend the same place; do not add a new middleware.
- Config: `backend/internal/config/config.go` `Load()` with env validation (see the `AO_ALLOWED_ORIGINS` parsing at :282-299 for the style). Tests in `config_test.go` list all `AO_*` keys (:14); add the new one.
- Auth tests: `backend/internal/httpd/auth_test.go` (the lockout table tests).

## Subtasks
- [ ] `config.go`: field `RemoteTrustedProxy bool` from `AO_REMOTE_TRUSTED_PROXY` (accepts `1/true/on`), default false. Add it to the env list in `config_test.go`.
- [ ] Pass the flag to `authMiddleware` (via `LANManager` or the `Server` options, wherever the CF logic is passed today).
- [ ] In `authMiddleware`: if the flag is on and `X-Forwarded-For` contains a valid IP, `src = first IP`. Otherwise fall back to the current behavior. Do not change the `CF-Connecting-IP` logic.
- [ ] Log a warning once at startup: "trusted proxy mode on; only enable behind a reverse proxy".
- [ ] Table tests: no flag → RemoteAddr; flag + valid XFF → XFF; flag + invalid/missing XFF → RemoteAddr; flag with two clients behind one peer → independent lockout.
- [ ] Document it in `docs/self-hosted-remote.md`, section "Behind a reverse proxy".

## Acceptance criteria
- [ ] `go test ./internal/httpd/...` green, covering all four cases.
- [ ] `make lint` clean.

## Verification
`cd backend && go test ./internal/httpd/ -run 'Auth|Lockout' -v`
