# RW-1.2.2 · Task: Worker entrypoint
**Status:** ✅ done (#65)

**Type:** infrastructure + shell. **Estimate:** 1 pd. **Story:** RW-1.2.

## Goal
A script that, on every start, prepares the volume, enables the remote-host listener without an interactive command, and runs `ao daemon` in the foreground.

## Context
- `ao remote-host enable` has no `--port` flag (`backend/internal/cli/remote_host.go:116-117`). The port comes only from `lastPort` in `<AO_DATA_DIR>/mobile/config.json`.
- `RestoreOnBoot` (`backend/internal/httpd/controllers/mobile.go:711-748`) reads `State{Enabled, Password, LastPort, LoopbackOnly, AccountTokenHash,...}` (`backend/internal/mobilebridge/config.go`) and starts the listener at boot.
- The password is 8 alphanumeric characters, plaintext, 0600. For a public URL a 256-bit account token is better (`POST /api/v1/remote-host/account-token`, `router.go:213-227`).
- The daemon refuses to start if `running.json` points to a live daemon (`daemon.go:211-224`); after a crash the file is stale and gets cleaned up by `runfile.CheckStale`.
- `ao daemon` is a hidden foreground command (`cli/root.go:349-359`).

## Subtasks
- [ ] Create `/data/ao`, `/data/home` if missing; `chmod 700`.
- [ ] If `AO_REMOTE_PASSWORD` is set and `mobile/config.json` is missing, write it: `{"enabled":true,"password":"$AO_REMOTE_PASSWORD","lastPort":${AO_REMOTE_PORT:-3011}}` with mode 0600. If the env var is also missing, generate a password and log it once (masked after the first start).
- [ ] If `AO_REMOTE_ACCOUNT_TOKEN` is set: after the daemon starts, call `POST 127.0.0.1:3001/api/v1/remote-host/account-token` (see the format in `controllers/mobile.go` IssueAccountToken) or write `accountTokenHash` directly (sha256 hex) into config.json. Pick one and document it.
- [ ] Credentials from env into `$HOME`: `GH_TOKEN` → `gh auth setup-git` (or leave `GH_TOKEN` as is, `gh` reads it); `git config --global user.name/email` from `AO_GIT_USER_NAME/EMAIL`; Claude: `ANTHROPIC_API_KEY` or `CLAUDE_CODE_OAUTH_TOKEN` pass through directly (see `adapters/agent/claudecode/auth.go:33-93`).
- [ ] Start `exec ao daemon` (PID 1 or under `tini`/`--init`) so it receives SIGTERM and shuts down cleanly.
- [ ] Log at startup: hostId (`GET 127.0.0.1:3001/api/v1/identity` after `/readyz`), port, whether the listener is dual-stack (RW-1.3.2).

## Acceptance criteria
- [ ] A container without a volume starts and shows the password in the log. A container with a volume and an existing config.json does not change the password or the hostId.
- [ ] `curl -H "Authorization: Bearer $PW" :3011/api/v1/projects` → 200 `[]`.
- [ ] `docker stop` completes in < 10 s with no leftover `ao pty-host` processes (`ps`).

## Verification
Manual, using the commands above + a shell test `deploy/railway/worker/entrypoint_test.sh` with `bats` or plain bash that checks the pre-seed logic without starting the daemon (`AO_ENTRYPOINT_DRY_RUN=1`).
