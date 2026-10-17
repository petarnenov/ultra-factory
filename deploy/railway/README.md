# Railway worker: operator runbook

This runbook supports RW-1.4.2 ([issue #17](https://github.com/petarnenov/ultra-factory/issues/17)).
The verification log below records sanitized, read-only Railway checks supplied
by RW-1.4.1 on 2026-10-17. Database continuity across a deliberate redeploy,
lockout isolation, operational metrics, and the RW-1.4.2 desktop/CLI end-to-end
flows remain unverified.

## Prerequisites and handoff

- Obtain the worker's verified Railway project, environment, service, source
  revision/image digest, host ID, public HTTPS domain targeting authenticated
  port `3011`, and evidence of a persistent volume at `/data` from the
  RW-1.4.1 verification log below. Recheck the deployment before pairing because
  service state can change.
- Use the image built from [worker/Dockerfile](worker/Dockerfile). Build context,
  pinned tools, local image checks, and entrypoint details are documented in
  [worker/README.md](worker/README.md). Leave the service start command empty so
  the image entrypoint runs. Set `RAILWAY_RUN_UID=0`; the entrypoint prepares the
  root-owned volume and drops to uid/gid `10001` before running the daemon.
- A desktop app and CLI compatible with the deployed worker revision. The desktop
  app is the recommended installation path. Enable Developer mode under Settings
  → General and sign in to AO Cloud to use Settings → Remote hosts, as described
  in [self-hosted remote hosts](../../docs/self-hosted-remote.md).
- An operator-approved test repository, Git identity, permission to push a test
  branch and create a PR, and that project's build dependencies on the worker.
  Agent and GitHub authentication belong on the worker; laptop credentials are
  not forwarded. Supply secrets privately through the service configuration.

Before pairing, the operator must verify public routing to `3011`, rejection
of unauthenticated protected API requests, and that `3001` is not published.
`GET /api/v1/identity` is the exact unauthenticated exception; compare its host
ID with the worker's locally reported identity. Record private IPv4 and IPv6
connectivity separately. Repository implementation and configuration claims do
not prove Railway reachability. Do not expose the loopback listener to fix it.

## Railway verification log 2026-10-17

Checks were performed read-only against project `giving-success`, environment
`production`, service `ultra-factory`. No Railway resources were created,
deleted, restarted, redeployed, or reconfigured. The Railway CLI credential
remained local. The service's configured bearer credential was supplied only to
authenticated HTTPS probes to this service. Neither value was printed or
recorded.

| Check | Status | Sanitized evidence |
| --- | --- | --- |
| Railway target | Pass | Project `giving-success` (`a1a58d18-7935-41fd-b930-5ee98621f246`), environment `production` (`a885c9c4-51e2-4ac4-9042-79d0f9989d85`), service `ultra-factory` (`0e7ebe89-bf3a-492c-a8f2-1a9ea22bc14c`). |
| Active deployment | Pass | Deployment `d5cd05c7-18ad-4b6c-9244-8059b4418943` is `SUCCESS`/`RUNNING`, created `2026-10-17T00:25:36.213Z`, commit `0f1d9f4c4aca8010e903755349c4a10d586188a0` on `feat/railway-hosting`, image digest `sha256:118d402e09183292688dbf1d89428c895e69f22047d37fae3381bda64e216931`. |
| Source/build | Pass | Repository `petarnenov/ultra-factory`, Dockerfile `deploy/railway/worker/Dockerfile`, root build context (`rootDirectory=null`). |
| Volume | Pass, persistence configured | `ultra-factory-volume` is `READY`, 50 GB, mounted at `/data`. Read-only metadata showed `/data/ao/ao.db` present (2,154,496 bytes); database contents were not read. |
| Public HTTPS and identity | Pass | `https://ultra-factory-production.up.railway.app/api/v1/identity` returned HTTP 200 with TLS verification result 0, API version 1, and host ID `h_a7b1467b-8c11-4dc5-b57b-9ddfe7c8863f`. |
| Readiness and authenticated route | Pass | On HTTPS, `/readyz` and `/api/v1/sessions` returned 401 without authentication and 200 with the local bearer credential. No credential value was output. |
| Public routing | Pass | Domain `ultra-factory-production.up.railway.app` is `ACTIVE` and targets port `3011`. |
| Main listener port `3001` | Pass, loopback-only | Read-only `/proc/net/tcp` and `/proc/net/tcp6` inspection showed `127.0.0.1:3001` and no wildcard listener on that port at inspection time. |
| Remote listener port `3011` | Pass, dual-stack | `/proc/net/tcp6` showed a wildcard listener on 3011 and `bindv6only=0`; the configured `AO_REMOTE_BIND` is `dual` and `AO_REMOTE_TRUSTED_PROXY` is `1`. |
| Private-network reachability | Pass, self-route | From the service container, `ultra-factory.railway.internal` resolved to IPv4 and IPv6. Requests to `http://ultra-factory.railway.internal:3011/api/v1/identity` returned 200 over both families. A separate peer service was not created, so peer-to-peer reachability remains unverified. |
| Host identity across observed deployments | Pass, limited | Current host ID matches the prior observation before the latest deployment. This does not independently establish database continuity. |
| Database preservation across redeploy | Unverified | No redeploy or pre/post database marker/checksum comparison was performed. A deliberate redeploy and sanitized state comparison are needed. |
| Five-failure lockout and client isolation | Unverified | No wrong-password sequence was sent, to avoid locking out a client. Requires an isolated test source and a second source IP or equivalent sanitized logs. |
| Readiness timing, image size, idle memory | Unverified | No timed deployment or idle measurement was performed; image size was not available from these read-only checks. |

An earlier independent client probe on 2026-10-16 observed public identity HTTP
200 and timed out when probing public port 3001. The current in-container socket
inspection independently confirms that the main listener binds only to
loopback. No secret or password values are recorded here.

## Environment contract

| Variable | Repository behavior / operator action |
| --- | --- |
| `HOME` | Image default `/data/home`; agent home files and global Git configuration belong on the volume. |
| `AO_DATA_DIR` | Image default `/data/ao`; stores DB, managed worktrees and remote-host configuration. |
| `AO_RUN_FILE` | Image default `/data/ao/running.json`; daemon discovery file, not a backup of running processes. |
| `AO_HARNESS_UPDATE_CHECKS` | Image default `off`. |
| `RAILWAY_RUN_UID` | Set to `0` for the entrypoint's volume ownership preparation and privilege drop. |
| `AO_REMOTE_PASSWORD` | First-start pairing password. Later starts preserve existing `mobile/config.json`; changing this variable alone does not rotate it. Set explicitly to avoid logging a generated first-start password. |
| `AO_REMOTE_PORT` | First-start port, default `3011`. Existing configuration wins; inspect the actual startup listener port. |
| `AO_REMOTE_ACCOUNT_TOKEN` | Optional additional Bearer credential; entrypoint writes its SHA-256 hash on every start. Use a privately generated 256-bit value. Removing the variable revokes an entrypoint-managed hash on next start. |
| `AO_REMOTE_TRUSTED_PROXY` | Set to `1` only under the single trusted proxy conditions in [ADR 0005](../../docs/adr/0005-trusted-proxy-remote-host.md). Uses the rightmost valid `X-Forwarded-For` entry for lockout, never authorization. Direct private-network access must remain restricted to trusted services. |
| `AO_GIT_USER_NAME`, `AO_GIT_USER_EMAIL` | Entrypoint writes global Git identity under the worker's `HOME`. |
| `GH_TOKEN` or `GITHUB_TOKEN` | Read by `gh`; entrypoint runs `gh auth setup-git` for HTTPS Git authentication. Supply permissions for the chosen test repository. |
| `ANTHROPIC_API_KEY`, `CLAUDE_CODE_OAUTH_TOKEN`, `ANTHROPIC_AUTH_TOKEN` | Credential inputs read by Claude Code; choose the approved authentication method. Entrypoint does not persist their values. |
| `AO_PORT` | Internal loopback API port, default `3001`. Leave at default for the image's fixed-port healthcheck. |

The image healthcheck calls loopback `/healthz`; the startup helper waits on
`/readyz` and logs the host ID and actual remote listener port. These do not
prove public routing, authentication, agent readiness, or terminal connectivity.
If port `3011` is occupied, the listener may choose an ephemeral port; resolve
the conflict instead of assuming Railway's public target follows that change.

## Desktop end-to-end procedure (human required; not run)

1. In Settings → Remote hosts, add name `worker-1`, the exact HTTPS worker URL,
   and its pairing password. Do not paste credentials into reports. Confirm it
   is online and the identity matches the worker. Saved desktop connections are
   kept in `~/.ao/remotes.json` (or `AO_DATA_DIR/remotes.json`).
2. Select that host and confirm navigation uses the `host.$hostId` route family.
   Record the host ID and displayed remote context; do not mistake a local
   project/session for the worker's project/session.
3. Use Projects + in the remote host context to clone the approved repository
   over HTTPS into a directory on `/data` (for example `/data/home/projects`).
   This uses `POST /api/v1/projects/clone`. Confirm the registered project path
   is on the worker volume and record its project ID.
4. Start a Claude Code worker session in that remote project. Open Terminal,
   verify input and streamed output, then send a message in the session and
   confirm a response. Ask for a small approved change on a test branch.
5. In that remote session, inspect the diff, run its relevant checks, commit,
   push the test branch, and create a PR with `gh pr create` against the test
   repository's intended base. Record the PR URL and session ID. Authentication
   failure is a failed check, not permission to copy laptop credentials.
6. From the remote session, run `ao preview README.md` for a static file. Open
   a session artifact with relative assets. Check content, asset loading and
   host isolation through the desktop remote proxy's `*.localhost` origin.
7. If the test project has an existing managed preview configuration, start it
   on the worker with `ao preview start <configuration>`. Otherwise use its
   documented dev command and adopt its loopback HTTP URL with `ao preview`.
   Check page loading and WebSocket updates if the app uses them. Do not treat
   a worker's raw `localhost` URL as a laptop URL. Record failures separately
   for static files, artifacts, managed apps, assets and WebSockets.

## CLI repeat (human required; not run)

Run on the paired laptop. Replace `REMOTE_PROJECT_ID` and `REMOTE_SESSION_ID`
with IDs returned by the worker; they are not necessarily the laptop's IDs.

```bash
ao host ls
ao project ls --host worker-1
ao agent ls --host worker-1
ao spawn --host worker-1 --project REMOTE_PROJECT_ID --harness claude-code \
  --name railway-cli-check --prompt "Inspect the repository and report readiness; do not publish changes."
ao session ls --host worker-1
ao session get --host worker-1 REMOTE_SESSION_ID
ao send --host worker-1 --session REMOTE_SESSION_ID --message "Report your current working directory."
```

Require `online` in `ao host ls`. Keep `--host worker-1` on subsequent commands;
session IDs are scoped to a host. Record spawn, listing and message delivery
results independently of the desktop checks.

## Password rotation (operator only; not run)

Schedule a maintenance window and retain private Railway console access.
The CLI has `remote-host status`, `enable`, and `disable`; it has no password
rotation subcommand. Merely changing `AO_REMOTE_PASSWORD` on an initialized
volume has no effect. Do not delete the entire configuration to rotate it.

Inside the selected worker's Railway SSH shell, use the daemon's loopback
rotation endpoint:

```bash
curl --fail --silent --show-error -X POST http://127.0.0.1:3001/api/v1/mobile/regenerate
```

The response contains the new password: keep it private. Rotation revokes the
account credential; update the saved desktop connection password and re-pair the
AO account as needed. If `AO_REMOTE_ACCOUNT_TOKEN` is configured, rotate or
remove that variable as well: otherwise the entrypoint reinstates its old hash
on next start. Privately verify that old credentials fail and the new credential
works, then repeat after restart. Do not include either credential in evidence.

## Redeploy, restart and persistence (operator only; not run)

Drain active sessions, stop app preview processes and record pending work before
the operator redeploys the selected service. Use the same `/data` volume and
explicitly chosen image revision; never delete the volume as part of this test.
An authorized operator may use `railway redeploy` after selecting the correct
project, environment and service; this runbook has not executed that command.

The image places the DB, worktrees, remote-host configuration and home files on
`/data`. Their persistence depends on that same volume being mounted again.
Environment credentials must still be configured on the service. Files outside
the volume are not covered by this persistence contract. Live agent processes,
tmux/PTY processes, terminal connections and preview servers do not survive
container restart; persisted session records do not imply live sessions resume.

After redeploy, verify identity and pairing, project/worktree files, DB-backed
session history, Git identity and agent authentication. Then start a new session
and repeat terminal/message/preview checks. Record what survived and what had
to restart; Railway volume retention and recovery remain unverified here.

## Troubleshooting

Use the Railway service's logs and, with human access, `railway ssh` into the
explicitly selected worker. Inside that shell run `ao status` and
`ao remote-host status`. The latter prints the password: do not attach raw output
to an issue. Inspect `/healthz` and `/readyz` at `http://127.0.0.1:3001` privately.
No Railway CLI version or service selection is assumed by these instructions.

| Symptom | Repository-backed diagnostic |
| --- | --- |
| Entrypoint exits before daemon starts | Check `/data` mount and ownership; entrypoint requires writable `HOME` and `AO_DATA_DIR`, and points to `RAILWAY_RUN_UID=0`. |
| Loopback health passes but desktop is offline | Check public domain target against actual listener port and identity. Loopback health alone does not test Railway routing. |
| `unauthorized` | Update the paired password; changing the first-start environment variable alone does not change the stored password. |
| `identity-changed` or `needs-pairing` | Confirm the correct volume/installation and re-pair intentionally. Do not bypass the identity guard. |
| Temporary lockout | Five failed attempts trigger a one-minute source lockout. Verify proxy configuration against ADR 0005; never disable Bearer authentication. |
| Clone, agent or PR fails | Check credentials and project dependencies on the worker; the laptop's credentials are not forwarded. Avoid printing environment values. |
| Preview returns a managed-server notice | Remote proxy emits this for an app preview requiring a managed server; start the existing configuration in that worker session. |
| Remote control route returns 404 | Expected for the listener's blocked routes, including mobile controls, browser/desktop controls, side-chat launch, system install, Codex accounts and session `/preview/server`. Use operator loopback access where appropriate; do not publish port `3001`. |

## Outstanding evidence and RW-2 / RW-4 handoff

The RW-1.4.1 log above records the live Railway configuration and route checks.
Peer-service connectivity and database continuity across a redeploy remain
unverified. RW-1.4.2 desktop and CLI behavior also remains untested. Static
limitations are distinguished from live checks:

| Check / limitation | Current evidence | Follow-up |
| --- | --- | --- |
| Public HTTPS identity/TLS | HTTP 200, TLS verification, API version and host ID verified on 2026-10-17 | RW-1.4.2 host-scoped desktop flow |
| Main listener port `3001` | `/proc/net/tcp` shows loopback-only listener at inspection time; earlier public probes also timed out | Keep the loopback API unpublished |
| Target port `3011`, protected API auth | Active domain target and unauthenticated/authenticated route responses verified on 2026-10-17 | RW-1.4.2 desktop pairing and host-scoped flows |
| Private IPv4/IPv6 | DNS and self-route HTTP 200 verified over both address families; separate peer service not tested | RW-1.4.2 peer connectivity if needed |
| Desktop pairing and host-scoped clone/session/terminal/message/PR | Pending; verified worker, approved test repository and private pairing access are needed | RW-1.4.2, RW-4 end-to-end |
| CLI spawn/list/get/send | Not tested; depends on pairing and an approved test repository | RW-1.4.2, RW-4 end-to-end |
| Static preview/artifacts/app assets/WebSockets | Desktop proxy implements forwarding; Railway path not tested | RW-2 proxy work, RW-4 end-to-end |
| Remote browser/desktop controls, system install, Codex accounts, preview-server control | Intentionally blocked by `lan_listener.go`; not a measured Railway failure | RW-2 capability handling; RW-4 limitations documentation |
| Password/account-token rotation and restart | Behavior inspected in source; operational checks not run | RW-4 secrets/recovery |
| Volume contents and host identity | `/data` is mounted and contains the DB; host ID matched an earlier observation. No pre/post database comparison was made | RW-1.4.1 redeploy persistence check |
| Process loss, startup time, idle memory and image size | Not measured in this read-only verification | RW-1.4.1 follow-up and RW-4 backup/recovery |

For each remaining live check, record the date, deployed revision/digest, client
version, host/project/session IDs, expected and actual outcome, pass/fail or
partial status, and sanitized evidence (including the test PR URL). Carry each
failed or partial result into RW-2/RW-4 tracking before acceptance. The
verification log above is Railway-side evidence; it does not establish the
desktop/CLI end-to-end behavior or redeploy database continuity.

Repository sources for these instructions: the linked worker files,
`backend/internal/cli/remote_host.go`, `backend/internal/cli/remote_target.go`,
`backend/internal/httpd/lan_listener.go`, `backend/internal/httpd/controllers/mobile.go`,
`frontend/src/main/remote-proxy.ts`, and the desktop's
`frontend/src/renderer/components/settings/RemoteHostsSettings.tsx` and
`frontend/src/renderer/components/CreateProjectFlow.tsx`.
