# Railway deployment runbooks

## Hub

This section covers the hub image from [hub/Dockerfile](hub/Dockerfile), which
runs the AO daemon, hub BFF, and web SPA in one Railway service. Railway setup,
deployment, and all browser checks below are operator procedures; none have been
performed as part of this runbook. Keep every live result marked pending until
an operator records sanitized evidence.

### Create the service

1. Build and publish the hub image from the repository root using the
   instructions in [hub/Dockerfile](hub/Dockerfile), or select the published
   hub image tag. Do not put credentials in build arguments or the image.
2. Create a Railway service named `hub` from that image. Leave the start command
   empty so the image entrypoint runs. Attach one persistent volume at `/data`.
   The supervisor expects to initialize the volume as root and then run as uid
   `10001`; set `RAILWAY_RUN_UID=0` so it can prepare ownership and permissions.
3. Railway sets `PORT`; the BFF listens on it (the image default is `8080`).
   Configure the service's public domain to target `$PORT`, not daemon ports
   `3001` or `3011`. Railway's public domain/TLS provisioning and any custom
   domain DNS/TLS status are pending operator validation.
4. Generate the operator password hash locally. Enter the password without
   putting it in shell history, then supply it on stdin to the repository's
   helper; copy only the resulting hash to Railway's `AO_WEB_PASSWORD_HASH`
   variable:

   ```sh
   read -rsp 'Hub operator password: ' HUB_PASSWORD; printf '\n'
   printf '%s' "$HUB_PASSWORD" | npm --prefix packages/hub-bff run --silent hash-password
   unset HUB_PASSWORD
   ```

   The actual helper is `packages/hub-bff/scripts/hash-password.ts`, invoked by
   that npm script. Do not set `AO_WEB_PASSWORD` in Railway: although the BFF
   supports it for local development, the Railway service should receive only
   the salted scrypt hash. Never paste the password or hash into issue comments,
   logs, or validation evidence.

### Hub environment variables

The following names are read by the hub image or BFF. Defaults noted below come
from [hub/Dockerfile](hub/Dockerfile),
[hub-supervisor.sh](hub/hub-supervisor.sh), and
[`packages/hub-bff/src/config.ts`](../../packages/hub-bff/src/config.ts).

| Variable | Hub configuration |
| --- | --- |
| `PORT` | Set by Railway; BFF listen port. Image default `8080`. Public domain target must use this port. |
| `RAILWAY_RUN_UID` | Set to `0` so the supervisor can prepare `/data` ownership, then drop privileges to uid/gid `10001`. |
| `AO_WEB_PASSWORD_HASH` | Required. Locally generated salted scrypt hash for operator login. Rotate by replacing this variable and redeploying. |
| `AO_WEB_ORIGIN` | Optional public origin, for example `https://hub.example.com`; when set, state-changing requests must match it. |
| `AO_HUB_SPA_DIR` | Image default `/opt/ao/hub/dist-web`; must contain `index.html`. |
| `AO_HUB_DATA_DIR` | Image default `/data/hub`; contains `hosts.json`, including worker connection credentials. |
| `AO_DATA_DIR` | Supervisor default `/data/ao`; daemon data, database, worktrees, and mobile listener configuration. |
| `HOME` | Supervisor default `/data/home`; home files and Git configuration. |
| `AO_HUB_DAEMON_PASSWORD_FILE` | Image default `/data/ao/mobile/config.json`; supervisor seeds this file on first start and preserves its daemon listener password on restarts. |
| `AO_HUB_BFF_HOST` | Optional BFF listen address; code default `::` (all interfaces, dual-stack). |
| `AO_HUB_MAX_WORKERS` | Optional worker registration limit; default `5`, positive integer. |
| `RAILWAY_API_TOKEN` | Optional credential for Railway worker management. Supply only after the operator has verified required permissions and accepted the resource/deletion behavior; the RW-3.1.1 spike did not verify service or volume deletion permissions. |
| `RAILWAY_PROJECT_ID`, `RAILWAY_ENVIRONMENT_ID` | Required with the Railway API token for worker provisioning. |
| `AO_HUB_WORKER_IMAGE` | Optional worker image used for worker provisioning. |
| `AO_HUB_WORKER_URL_MODE` | Optional `public` or `private`; default `public`. Private Railway DNS connectivity is an open live check. |

`AO_HUB_OPERATOR_PASSWORD_HASH`, `AO_HUB_SESSION_SECRET`, and
`AO_HUB_DAEMON_URL` from the earlier task text are not read by the current
code. The operator hash is `AO_WEB_PASSWORD_HASH`; the hub entry uses the
daemon password file and its `lastPort` (normally `3011`) to form the internal
loopback URL. The BFF currently keeps opaque sessions in memory and has no
signing secret, so there is no session-secret variable to rotate. Treat the
requested session-secret design/rotation as an open implementation decision;
do not create or set a guessed variable. A BFF restart clears all browser
sessions.

### Add the RW-1.4 worker

After the hub is deployed and login works, open **Settings → Remote hosts** in
the hub UI and add the worker with label `worker-1`, its listener URL, and its
pairing password. Use
`http://worker-1.railway.internal:3011` only after the operator verifies that
the hub can resolve and reach that private endpoint (including Railway's
private-network address family). The Railway spike did not verify this. If
private networking does not work, use the worker's public HTTPS URL and verify
that its public domain targets the authenticated listener on `3011`; never
expose the hub daemon's `3001` listener. Confirm `/hub/hosts` contains the hub
entry and the worker entry, and that the worker reports online. Do not include
pairing credentials in screenshots or logs.

### Data layout, backup, and recovery

The hub image places durable state under the mounted volume:

| Path | Contents |
| --- | --- |
| `/data/ao` | Daemon database, worktrees, `running.json`, and `mobile/config.json` for the loopback-only listener credential. |
| `/data/hub/hosts.json` | Host registry, including worker connection credentials; stored with restrictive permissions. |
| `/data/home` | Hub home directory. |

Before a backup, stop or quiesce the service so SQLite and registry writes are
not in flight, then use the operator-approved Railway volume backup/snapshot
procedure and record its timestamp and volume identity. Restore only to an
operator-selected volume and verify file ownership, permissions, login, host
registry, daemon readiness, and worker reachability before reopening traffic.
The exact Railway backup/restore mechanism and volume recovery behavior were
not established by the Railway API spike and remain pending operator
validation. A volume backup does not preserve live agent processes, terminals,
SSE connections, or in-memory browser sessions.

### Rotate credentials and update the image

- **Operator password:** generate a new hash with the command above, replace
  `AO_WEB_PASSWORD_HASH` in Railway, and redeploy. The process restart clears
  all in-memory sessions; verify the new password works and the old password
  fails. Do not change `AO_WEB_PASSWORD` or use a plaintext Railway variable.
- **Session secret:** there is no session signing secret in the current BFF.
  Restarting invalidates sessions because they are in memory. A distinct secret
  rotation procedure remains an open item until session signing is implemented.
- **Hub daemon credential:** keep `/data/ao/mobile/config.json` with the volume;
  the supervisor seeds the listener as loopback-only on port `3011` and retains
  the generated password. Never publish either daemon port. Credential rotation
  behavior for the hub BFF's automatically read hub entry should be separately
  validated before changing that file.
- **Image update:** confirm no active hub sessions, stop new work, and record
  pending work before redeploying. Restarting the hub terminates live daemon
  processes and terminals, even though `/data` persists. Select the intended
  image revision and reattach the same `/data` volume; after startup, verify
  `/healthz`, the board, `/hub/hosts`, and a new terminal session. Do not infer
  recovery of active work from persisted session records.
- **Rollback:** if the new image fails these checks, select the previously
  recorded working image revision and redeploy it with the same `/data` volume
  and compatible service variables. Verify health, login, host entries, and a
  new terminal before reopening normal use. Do not roll back by deleting or
  replacing the volume. This procedure is pending a human deployment exercise.

### Logs and validation record

Use the selected Railway hub service's deployment logs. The supervisor writes
startup and process-exit messages to stderr; the BFF writes structured JSON
request logs to stdout and excludes passwords, hashes, cookies, and headers.
Do not share unredacted `mobile/config.json`, environment dumps, or logs that
could reveal credentials.

Record these checks after deployment. All values are **pending human validation**
until an operator supplies the dated result, image revision, and sanitized
evidence. No Railway deployment or live browser test was performed for this
runbook.

| Check | Result | Evidence to record |
| --- | --- | --- |
| Public domain and TLS | Pending human validation | Domain, HTTP→HTTPS behavior, certificate result, and target port `$PORT`. |
| Login and SPA board | Pending human validation | Browser/client and measured SPA load time; confirm login and hub board render. |
| Hub and worker hosts | Pending human validation | `/hub/hosts` shows the hub entry and `worker-1`; record online/offline state and whether the private or public worker URL was used. |
| SSE | Pending human validation | Measure event delivery latency from an approved board update; include method and client location. |
| Terminal | Pending human validation | Open an approved worker session, send input, and confirm streamed output; live PTY/terminal is not expected to survive restart. |
| Persistence and restart | Pending human validation | Verify `/data` contents and host registry after restart; separately record what live processes stopped. |
| Image update and rollback | Pending human validation | Record old/new image revisions, no-active-session check, volume attachment, rollback procedure, and outcome. |
| Backup and restore | Pending human validation | Record the approved backup/snapshot and restore test; API spike did not verify it. |

The spike in [`packages/hub-bff/docs/railway-api-notes.md`](../../packages/hub-bff/docs/railway-api-notes.md)
confirmed only selected Railway GraphQL create/configuration operations with a
public test image. It did not deploy this hub image or verify hub startup,
private worker networking, image timing, volume persistence/backup, or deletion
permissions. Those items remain open and must not be reported as passed.

This runbook supports RW-1.4.2 ([issue #17](https://github.com/petarnenov/ultra-factory/issues/17)).
The log below corrects uncorroborated Railway claims previously added on this
PR branch. Independent public probes are recorded below with their limited scope. Railway
configuration and private-network checks are **unverified / pending**; no
readable sanitized RW-1.4.1 Railway report has been provided.
Desktop and CLI end-to-end checks also remain outstanding.

## Prerequisites and handoff

- Obtain the worker's verified Railway project, environment, service, source
  revision/image digest, host ID, public HTTPS domain targeting authenticated
  port `3011`, and evidence of a persistent volume at `/data` from RW-1.4.1.
  Previously recorded target/configuration values are unverified claims, not
  confirmation of the current deployment.
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

## Evidence status correction 2026-10-17

The handoff text was added on PR #83's `rw/RW-1.4.2` branch, whose head was
`79fbf301aa8ba98572b62c7da65c5085366a596b` before this correction. Its Railway
pass statements are not corroborated by a verifiable RW-1.4.1 report or an
accessible direct Railway verification. They must not be treated as test results.
The user confirmed that the Z600 RW-1.4.2 session owns this branch; the separate
iMac RW-1.4.1 session is issue #16 on `ao/ultra-factory-14/root`. AO's listing of
PR #83 under that iMac session does not establish provenance for Railway checks.
Issue #16 has no handoff comment, and the requested iMac read-only verification
has not returned a readable sanitized Railway report. No secret values are
needed as evidence.

The user supplied independent read-only public probe results dated
2026-10-16 22:18 UTC from the orchestrator client, with no credentials used or
exposed. HTTPS
`GET https://ultra-factory-production.up.railway.app/api/v1/identity` returned
HTTP 200 with TLS verification result `0`. Direct HTTP and HTTPS probes to that
hostname on port `3001` both timed out (curl status `000`) from that client.
These establish
public endpoint response and observed port unreachability from that client,
not Railway routing configuration or universal port isolation. The host ID
comparison against worker-local identity remains pending.

| Check | Status | Required evidence from RW-1.4.1 |
| --- | --- | --- |
| Railway target | Unverified / pending | Confirm project, environment and service identifiers against the actual target. |
| Source/build | Unverified / pending | Deployed source revision, repository/branch, root build context and worker Dockerfile. |
| Volume | Unverified / pending | Actual `/data` mount and volume readiness; restart persistence is a separate check. |
| Public HTTPS/TLS | Confirmed by independent public probe | At 2026-10-16 22:18 UTC, the supplied public hostname responded over HTTPS with successful TLS verification. Configured target port `3011` remains unverified. |
| Public identity | HTTP 200 confirmed by independent public probe | At 2026-10-16 22:18 UTC, the public identity endpoint returned 200 without credentials. Worker-local host ID comparison remains pending. |
| API authentication | Unverified / pending | Sanitized unauthenticated rejection and authenticated success results; no credentials or response payloads. |
| Public port `3001` | HTTP and HTTPS probes timed out from probe client | Independent probes at 2026-10-16 22:18 UTC establish only observed unreachability from that client. Railway routing configuration and general port isolation remain unverified. |
| Private IPv4/IPv6 | Unverified / pending | Separate DNS and HTTP connectivity results for each address family, with probe origin. |
| Deployment | Unverified / pending | Actual deployment ID/state, source revision/image digest, readiness timing and image size. |
| Desktop pairing and remote flow | Pending | Verified worker, private pairing access and an approved test repository. |
| CLI remote flow | Pending | Pairing and an approved test repository. |

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

Independent public probe results have the limited scope recorded above. Railway
configuration/private-network checks await corroborating RW-1.4.1 evidence.
RW-1.4.2 desktop and CLI behavior also remains untested. Static limitations are
distinguished from live checks:

| Check / limitation | Current evidence | Follow-up |
| --- | --- | --- |
| Public HTTPS identity/TLS | Independent probe confirmed HTTP 200 and TLS verification at 2026-10-16 22:18 UTC; host identity comparison pending | RW-1.4.1, RW-4 networking/security |
| Public port `3001` | HTTP/HTTPS probes timed out from the probe client; configured isolation remains unverified | RW-1.4.1, RW-4 networking/security |
| Target port `3011`, protected API auth and private IPv4/IPv6 | Unverified / pending; no corroborated RW-1.4.1 report | RW-1.4.1, RW-4 networking/security |
| Desktop pairing and host-scoped clone/session/terminal/message/PR | Pending; verified worker, approved test repository and private pairing access are needed | RW-1.4.2, RW-4 end-to-end |
| CLI spawn/list/get/send | Not tested; depends on pairing and an approved test repository | RW-1.4.2, RW-4 end-to-end |
| Static preview/artifacts/app assets/WebSockets | Desktop proxy implements forwarding; Railway path not tested | RW-2 proxy work, RW-4 end-to-end |
| Remote browser/desktop controls, system install, Codex accounts, preview-server control | Intentionally blocked by `lan_listener.go`; not a measured Railway failure | RW-2 capability handling; RW-4 limitations documentation |
| Password/account-token rotation and restart | Behavior inspected in source; operational checks not run | RW-4 secrets/recovery |
| Volume persistence, process loss, startup time and image digest/size | Unverified / pending; no corroborated measurements | RW-1.4.1 follow-up and RW-4 backup/recovery |

For each live check, the operator must record date, deployed revision/digest,
client version, host/project/session IDs, expected and actual outcome, pass/fail
or partial status, and sanitized evidence (including the test PR URL). Carry
each failed or partial result into RW-2/RW-4 tracking before acceptance. The
table above is a handoff checklist, not evidence that those backlog items were
updated. A second person must follow this runbook and record any missing steps.

Repository sources for these instructions: the linked worker files,
`backend/internal/cli/remote_host.go`, `backend/internal/cli/remote_target.go`,
`backend/internal/httpd/lan_listener.go`, `backend/internal/httpd/controllers/mobile.go`,
`frontend/src/main/remote-proxy.ts`, and the desktop's
`frontend/src/renderer/components/settings/RemoteHostsSettings.tsx` and
`frontend/src/renderer/components/CreateProjectFlow.tsx`.
