# Railway deployment runbooks

## Hub

This section covers the hub image from [hub/Dockerfile](hub/Dockerfile), which
runs the AO daemon, hub BFF, and web SPA in one Railway service. Railway setup,
deployment, and the browser checks below are operator procedures. The first
live deployment and its sanitized results are recorded under
[Logs and validation record](#logs-and-validation-record) (RW-2.5.3, issue
#104); keep any check not listed there as passed marked pending.

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
| `AO_HUB_MAX_VOLUME_GB` | Optional upper bound, in GB, for a worker volume size in a `POST /hub/workers` spec. Unset means no limit. See [Limits and cost monitoring](#limits-and-cost-monitoring). |
| `AO_HUB_IDLE_STOP_HOURS` | Optional. Hours without an active session before an online Railway worker shows an idle warning in Settings → Workers. Unset (default) turns the check off. Warning only; nothing is stopped. |
| `RAILWAY_API_TOKEN` | Optional credential for Railway worker management. Supply only after the operator has verified required permissions and accepted the resource/deletion behavior; the RW-3.1.1 spike did not verify service or volume deletion permissions. |
| `RAILWAY_PROJECT_ID`, `RAILWAY_ENVIRONMENT_ID` | Required with the Railway API token for worker provisioning. |
| `AO_HUB_WORKER_IMAGE` | Optional worker image used for worker provisioning. |
| `AO_HUB_WORKER_URL_MODE` | Optional `public` or `private`; default `public`. Private Railway DNS connectivity is an open live check. |
| `AO_HARNESS_UPDATE_CHECKS` | Image default `on` for the hub (the worker image keeps `off`), so Settings → Harness → This computer reports updates and offers Update. |
| `NPM_CONFIG_PREFIX`, `AO_HUB_NPM_SEED` | Image defaults `/data/home/.npm-global` and `/opt/ao/npm-global`. The image installs Claude Code and Codex under the seed; the supervisor copies them into the volume prefix when it lacks them or has an older version. `PATH` searches the volume prefix first, so a harness update from the UI installs there and persists across restarts and image updates. |
| `ANTHROPIC_API_KEY` | Optional. Claude Code credential for an orchestrator in the hub, read by `claude` from the environment. See [Orchestrator agent credentials](#orchestrator-agent-credentials). |
| `CLAUDE_CODE_OAUTH_TOKEN` | Optional alternative to `ANTHROPIC_API_KEY` for Claude Code. Set at most one of the two. |
| `OPENAI_API_KEY` | Optional. Codex API key; used only for the one-time `codex login --with-api-key` below, not read by AO's readiness probe. |
| `AO_HUB_ORCHESTRATOR_TICK` | Optional. `1` starts the orchestrator wake-up loop under the supervisor; default off. See [Orchestrator wake-up loop](#orchestrator-wake-up-loop). The tick also reads the `RW_TICK_*` variables documented in its script header. |
| `GH_TOKEN` | Required for an orchestrator in the hub. Fine-grained token limited to this repository; read by `gh` and, through `gh auth token`, by the daemon's GitHub adapters. |

`AO_HUB_OPERATOR_PASSWORD_HASH`, `AO_HUB_SESSION_SECRET`, and
`AO_HUB_DAEMON_URL` from the earlier task text are not read by the current
code. The operator hash is `AO_WEB_PASSWORD_HASH`; the hub entry uses the
daemon password file and its `lastPort` (normally `3011`) to form the internal
loopback URL. As specified by [ADR 0006](../../docs/adr/0006-web-bff-for-hub.md),
sessions are opaque random IDs held in BFF memory; there is no signing secret.
A BFF redeploy clears all sessions by design.

### Orchestrator agent credentials

An orchestrator started in the hub runs one of its agent CLIs: `claude` and
`codex` from the hub's updatable prefix (see `NPM_CONFIG_PREFIX` above), or
`opencode` inherited from the worker image. It needs a
provider credential for its harness and a GitHub token. The operator sets
them; no credential goes into the repository, the image, build arguments, or
logs. The hub supervisor does not log credential values. Unlike the worker
entrypoint, it does not log credential variable names either and does not run
`gh auth setup-git` (see the GitHub token below).

There are two ways to authenticate a harness:

- **Railway variable.** The value lives in Railway's variable store and
  reaches the daemon and every agent process through the environment.
  Nothing is written to the volume. Rotation is a variable change and a
  redeploy.
- **One-time interactive login in a hub terminal.** The CLI writes its own
  credential file under `HOME=/data/home` on the volume, so the login
  survives redeploys. Rotation is a new login. The file is in every volume
  snapshot. Run the login as uid `10001` with `HOME=/data/home`, for example
  from a terminal session on the hub host in the web UI. A Railway shell may
  start as root with another `HOME`; in that case wrap the command in
  `setpriv --reuid=10001 --regid=10001 --init-groups env HOME=/data/home …`,
  otherwise the file lands in the wrong place or is unreadable to the daemon.

Per harness:

| Harness | Chosen method | Alternative | Where `ao agent ls` looks |
| --- | --- | --- | --- |
| `claude-code` | `ANTHROPIC_API_KEY` as a Railway variable on the `hub` service. | `CLAUDE_CODE_OAUTH_TOKEN` as a variable (created locally with `claude setup-token`; check the provider's terms for subscription use on a server), or `claude` then `/login` in a hub terminal, which writes `/data/home/.claude/.credentials.json`. | Environment first, then the credentials file. |
| `codex` | One-time login in a hub terminal: `codex login --device-auth` for a ChatGPT account, or `printenv OPENAI_API_KEY \| codex login --with-api-key` for an API key. Both write `/data/home/.codex/auth.json`. | None. AO's probe runs `codex login status`, which reads `auth.json`, so an `OPENAI_API_KEY` variable without the login step is not expected to report `authorized`. Remove `OPENAI_API_KEY` from Railway after the login if it is not needed for anything else. | `codex login status`. |

Pick one harness for the orchestrator and set up only its credential. The
other harnesses can stay unauthorized.

**GitHub token (`GH_TOKEN`).** The orchestrator reads and edits issues,
comments on them, opens, reviews and merges pull requests with `gh pr merge`,
and the daemon polls PR and CI facts. Create a fine-grained personal access
token with **Repository access: Only select repositories →
`petarnenov/ultra-factory`** and these repository permissions:

| Permission | Access | Why |
| --- | --- | --- |
| Metadata | Read | Mandatory for every fine-grained token. |
| Issues | Read and write | `gh issue view`, issue comments, editing task checklists and labels. |
| Pull requests | Read and write | `gh pr view/create/review/comment` and `gh pr merge`. |
| Contents | Read and write | Merging a PR writes to the base branch; clone and fetch read it; `git push` writes branches. |
| Commit statuses | Read (optional) | CI state for PR facts and `gh pr checks`. Confirm in the live check whether it is needed. |
| Workflows | Read and write (optional) | Only if the orchestrator must merge PRs that change `.github/workflows/`. Without it those merges fail and stay with a human. |

Grant nothing else: no account permissions, no other repositories, no admin.
The token cannot bypass branch protection. Set an expiry and record it. `gh`
reads `GH_TOKEN` directly, and the daemon's `gh auth token` fallback returns
it, so `gh auth login` is not needed and should not be run (it would write
the token to `/data/home/.config/gh/hosts.yml`). For `git` over HTTPS with
the same token, run `gh auth setup-git` once in a hub terminal as uid
`10001`; it writes only a credential-helper reference to
`/data/home/.gitconfig`, not the token.

**Rotation.** Railway variables: create the new key or token, replace the
variable on the `hub` service, redeploy when no hub session is active (see
**Image update** below: a restart ends live agents), confirm `ao agent ls`
and `gh auth status` in the hub, then revoke the old credential at the
provider. Interactive logins: log in again in a hub terminal (for Codex,
`codex logout` first), confirm `ao agent ls`, then revoke the old session or
key at the provider. A credential that may have leaked is revoked first and
replaced second.

**Live check (operator, pending).** In a hub terminal, `ao agent ls` lists
the chosen harness as `authorized`; an orchestrator session started in the
hub answers a prompt and runs
`gh issue view 119 --repo petarnenov/ultra-factory`. Record only the result
and the date, never `gh auth status -t` output or environment dumps.

### Orchestrator wake-up loop

Worker reports reach a hub orchestrator through the ADR 0005 pull, but no
report covers PR and CI state (a PR that turns green, a merge conflict). On
the Mac, launchd runs
[`orchestrator-tick.sh`](../../docs/backlog/railway/tools/orchestrator-tick.sh)
every 5 minutes for that. In the hub the same script runs as
`/usr/local/bin/ao-orchestrator-tick.sh loop`, a third child of the
supervisor, when the `hub` service has `AO_HUB_ORCHESTRATOR_TICK=1`.

- **Why not `ao automation`.** An automation run spawns a new session with a
  prompt; it cannot run a script or send a message to an existing
  orchestrator. A supervised loop needs no daemon change and nothing on the
  operator's machines.
- **What one tick does.** It finds the single live orchestrator of
  `RW_TICK_PROJECT` (default `ultra-factory`) with `ao orchestrator ls`. Zero
  or several live orchestrators, or a daemon that is not ready, are only
  logged as `VIOLATION`; nothing is sent. An orchestrator in an active turn is
  never interrupted. Otherwise, when the snapshot of worker sessions, open PRs
  to `feat/railway-hosting` and invariants changed, or work is in flight and
  the last nudge is older than 30 minutes (10 with a violation), it sends a
  `TICK` message with `ao send`. A green mergeable PR therefore produces a
  `TICK` on the next tick, within 5 minutes.
- **GitHub.** `gh` reads `GH_TOKEN` (see
  [Orchestrator agent credentials](#orchestrator-agent-credentials)); no
  `gh auth login` is needed. Without a checkout in the image, the
  "integration branch contains main" check uses the GitHub compare API. A
  missing or rejected token is logged as `VIOLATION`.
- **Hosts.** `RW_TICK_HOSTS` (default `Linux IMac Z600`) names the hosts whose
  `rw/` sessions go into the snapshot, through `ao session ls --host`.
- **Log and state.** `/data/ao/rw-tick/` (`$AO_DATA_DIR/rw-tick`):
  `tick.log`, the last snapshot and the last-sent state. Each log line is
  also printed to the service log. The `TICK` message itself is not logged.
- **Start.** The first tick runs 60 seconds after the container starts
  (`AO_HUB_TICK_START_DELAY`), so the daemon is ready.
- **Failure.** A failed tick is logged and the loop continues. If the loop
  process exits, the supervisor logs it and restarts it after 60 seconds; the
  daemon and the BFF keep running.

**Pause for a redeploy or maintenance.** In a hub terminal (uid `10001`,
`HOME=/data/home`):

```bash
ao-orchestrator-tick.sh pause "redeploy"   # writes /data/ao/rw-tick/paused
ao-orchestrator-tick.sh status             # shows the flag and the last ticks
ao-orchestrator-tick.sh resume             # removes the flag
```

The flag is on the volume, so it survives the redeploy; resume after the
hub is verified. While it is set every tick logs `paused (...)` and sends
nothing. Setting `RW_TICK_PAUSED=1` on the service pauses as well, but changing
a variable restarts the hub. To turn the loop off for good, remove
`AO_HUB_ORCHESTRATOR_TICK`.

**Offline check.** `bash docs/backlog/railway/tools/orchestrator-tick-test.sh`
runs ticks with `--dry-run` against fixtures, with fake `ao` and `gh`.

**Live check (operator, pending).** With `AO_HUB_ORCHESTRATOR_TICK=1`, a hub
orchestrator running, and the Mac's launchd tick stopped
(`orchestrator-tick.sh uninstall` on the Mac), a green mergeable PR to
`feat/railway-hosting` produces a `nudged <orchestrator> (state changed)` line
in `/data/ao/rw-tick/tick.log` and a `TICK` message in the orchestrator within
10 minutes. Run only one tick per orchestrator: the Mac and the hub must not
both nudge.

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
| `/data/hub/audit.jsonl` | Append-only security events; mode `0600`, no secrets. |
| `/data/home` | Hub home directory, including any interactive agent logins (`.claude/.credentials.json`, `.codex/auth.json`). |

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
  `AO_WEB_PASSWORD_HASH` in Railway, then redeploy the BFF. Replacing the
  operator password and redeploying invalidates all sessions because sessions
  are opaque IDs held in memory; this is the intended behavior. Verify the new
  password works and the old password fails. Do not change `AO_WEB_PASSWORD`
  or use a plaintext Railway variable. There is no session signing secret to
  configure or rotate.
- **Railway API token:** create a project-scoped token for the configured
  project/environment, replace `RAILWAY_API_TOKEN` in Railway, and redeploy.
  Startup checks Railway's `projectToken` identity against
  `RAILWAY_PROJECT_ID` and `RAILWAY_ENVIRONMENT_ID` and fails closed on a
  mismatch or account-wide credential. Revoke the old token after the new
  deployment passes the scope check. The token tested in RW-3.1.1 could not
  delete services or volumes; the permissions needed for complete worker
  cleanup remain unverified. Do not substitute an account-wide token. Keep
  worker creation disabled by leaving `RAILWAY_API_TOKEN` and its project IDs
  unset until an operator verifies deletion permissions for a project-scoped
  token.
- **Agent and GitHub credentials:** see **Rotation** under
  [Orchestrator agent credentials](#orchestrator-agent-credentials).
- **Hub daemon credential:** keep `/data/ao/mobile/config.json` with the volume;
  the supervisor seeds the listener as loopback-only on port `3011` and retains
  the generated password. Never publish either daemon port. Credential rotation
  behavior for the hub BFF's automatically read hub entry should be separately
  validated before changing that file.
- **Worker password:** the `ao remote-host` CLI has no password-rotation
  command. The daemon does provide `POST /api/v1/mobile/regenerate` on its
  loopback API at `127.0.0.1:3001`; the authenticated remote-host listener and
  BFF proxy intentionally block `/api/v1/mobile`, so
  `/hosts/:id/api/v1/mobile/regenerate` cannot be used. From the selected
  worker's private Railway shell, call the loopback route and keep its returned
  password private. The BFF has no route to update a host credential, so update
  only the matching worker's `password` field in `/data/hub/hosts.json` on the
  hub volume using an atomic replacement that preserves mode `0600`, then
  restart the BFF and probe the host. Do not delete `mobile/config.json` or rely
  on changing `AO_REMOTE_PASSWORD`: the worker entrypoint ignores that variable
  after first boot. Verify the old password fails and the new one works without
  putting either value in logs, shell history, or issue evidence.
- **Image update:** confirm no active hub sessions, stop new work, and record
  pending work before redeploying. With the wake-up loop on, pause it first
  and resume it after the checks below (see
  [Orchestrator wake-up loop](#orchestrator-wake-up-loop)). Restarting the hub terminates live daemon
  processes and terminals, even though `/data` persists. Select the intended
  image revision and reattach the same `/data` volume; after startup, verify
  `/healthz`, the board, `/hub/hosts`, and a new terminal session. Do not infer
  recovery of active work from persisted session records.
- **Rollback:** if the new image fails these checks, select the previously
  recorded working image revision and redeploy it with the same `/data` volume
  and compatible service variables. Verify health, login, host entries, and a
  new terminal before reopening normal use. Do not roll back by deleting or
  replacing the volume. This procedure is pending a human deployment exercise.

### Automated image publish and deploy

[`.github/workflows/hub-image.yml`](../../.github/workflows/hub-image.yml)
builds, smoke-tests, and publishes the hub image on every push to
`feat/railway-hosting` that touches its inputs (`backend/`, `frontend/`,
`packages/`, `deploy/railway/`, or the workflow). That branch publishes
`ghcr.io/<owner>/ao-hub:sha-<short>` for linux/amd64 only, then the deploy job
sets the Railway `hub` service image to that tag and waits for the deployment
to reach `SUCCESS`. Pushes to `main` and `hub-v*` tags still publish
multi-arch images and never deploy. Every deploy restarts the hub, as in
**Image update** above. To approve each deploy instead, add required reviewers
to the `railway-hub` environment.

One-time setup (operator):

1. Set the repository variable `AO_WORKER_TAG` to a published `ao-worker` tag
   that has a linux/amd64 image. The hub image extends it.
2. In each GHCR package's settings (`ao-hub`, `ao-worker`), under **Manage
   Actions access**, give this repository access: write for `ao-hub`, read for
   `ao-worker`. Packages first pushed from a laptop are not linked to the
   repository, and `GITHUB_TOKEN` cannot push or pull them otherwise.
3. Keep `ao-hub` pullable by Railway: public, or registry credentials on the
   service.
4. Create a Railway project token for the hub's project and environment
   (Project Settings → Tokens). In GitHub, create the `railway-hub`
   environment with the secrets `RAILWAY_TOKEN` (that token) and
   `RAILWAY_HUB_SERVICE_ID` (the `hub` service ID). Without them the deploy
   job is skipped with a notice and the image is still published.

The deploy uses Railway's `serviceInstanceUpdate`, `serviceInstanceDeployV2`,
and `deployment` GraphQL operations with the project token's
`Project-Access-Token` header. Its first run is the live check of those
operations; record the result in the validation record below.

### Limits and cost monitoring

Railway bills by usage: CPU, memory, network egress, volume storage, and
backups, per service. Every running worker costs money whether or not an agent
is working on it, so the hub has three guards:

- `AO_HUB_MAX_WORKERS` (default `5`) caps how many Railway workers the BFF
  registers; `POST /hub/workers` answers `429 worker_limit_reached` at the cap.
- `AO_HUB_MAX_VOLUME_GB` rejects a create request whose `size` exceeds the
  limit with `400 volume_size_limit`. Railway's `volumeCreate` takes no size
  ([RW-3.1.1 notes](../../packages/hub-bff/docs/railway-api-notes.md)), so
  every `size` is still refused, as `unsupported_size`, until sizing is
  verified; volumes get the plan default.
- `AO_HUB_IDLE_STOP_HOURS` makes the BFF check each online Railway worker's
  active sessions every 5 minutes. A worker with none for that many hours shows
  an "Idle" warning in Settings → Workers and a `warn` log line
  `idle Railway workers detected`. The clock is in memory: it restarts when the
  BFF restarts, and a failed session check clears it. **No worker is stopped
  automatically.** The verified Railway API has no stop/start that is known to
  keep the volume, so the operator removes an idle worker with the Remove
  dialog, or leaves it running.

#### Read Railway usage for the project

Read-only steps; they change nothing in the project.

1. Dashboard: open the workspace **Usage** page (account switcher, top left, or
   `railway.com/workspace/usage` for a personal account). It shows the current
   bill and an estimated bill for the current period, and ranks projects by
   cost. Railway's own advice is to run a project for about a week and
   extrapolate from the estimate.
2. CLI, while logged in as a workspace member:

   ```sh
   railway usage                                    # workspace summary
   railway usage projects --project <project>       # per-service CPU, memory, egress, volume, backup, total
   railway usage projects --project <project> --period previous
   railway usage limit status --target workspace    # current compute soft/hard limits
   ```

   Worker services are named `ao-worker-<id>`; the hub is the hub service. A
   worker that costs money but has no matching row in Settings → Workers is an
   orphan; it is listed under "Unregistered Railway services" and must be
   deleted in the Railway console.
3. Per service: the service **Metrics** tab shows CPU, memory, and disk over
   time. Use it to size replica limits (below).

Set a workspace spending guard as a workspace admin, in the Usage page or with
`railway usage limit set --target workspace --soft <dollars> --hard <dollars>`
(whole dollars). The soft limit only emails. **The hard limit takes every
workload in the workspace offline, the hub included,** and that stops live
agent sessions just like a redeploy does. Prefer a soft limit plus the worker
cap; set a hard limit only as a backstop well above the expected bill.

#### Recommended sizes

These are starting points, not measured values: no worker load was measured on
Railway for this runbook.

| Setting | Recommendation |
| --- | --- |
| `AO_HUB_MAX_WORKERS` | Keep the default `5` or lower until a week of usage shows the per-worker cost; raise it deliberately. |
| `AO_HUB_MAX_VOLUME_GB` | The plan's default volume size (Hobby 5 GB, Pro 50 GB, per the [volume reference](https://docs.railway.com/volumes/reference)), so a spec can never ask for more than Railway gives by default. |
| `AO_HUB_IDLE_STOP_HOURS` | `12` for a single operator; it flags a worker left running overnight without warning during a normal working day. |
| Hub replica limits | Leave unset for the first week, then set CPU and memory to about 1.5× the peak on the Metrics tab (service settings → Deploy → Replica Limits). Too low a memory limit crashes the service. |
| Worker replica limits | Same method, measured on a worker while it runs its usual number of parallel agent sessions; each Claude Code session plus its build/test commands is the main memory consumer. |
| Volumes | Hub: holds the database, `hosts.json`, and orchestrator worktrees. Worker: holds `HOME`, the AO database, and every worktree. Watch disk on the Metrics tab and grow the volume in the Railway console before it fills. |

### Logs and validation record

Use the selected Railway hub service's deployment logs. The supervisor writes
startup and process-exit messages to stderr; the BFF writes structured JSON
request logs to stdout and excludes passwords, hashes, cookies, and headers.
Do not share unredacted `mobile/config.json`, environment dumps, or logs that
could reveal credentials.

Record these checks after deployment with the date, image revision, and
sanitized evidence. The results below are from the first live deployment on
2026-10-17 (RW-2.5.3, [issue #104](https://github.com/petarnenov/ultra-factory/issues/104)),
with domains, IDs, and digests masked as in RW-1.4.1. Rows still marked pending
were not verified and must not be reported as passed.

| Check | Result | Evidence |
| --- | --- | --- |
| Public domain and TLS | Pass (2026-10-17) | New Railway-generated domain `<hub-domain>`: `/healthz` 200 over HTTPS with a valid certificate, HTTP → HTTPS 301, HSTS set. The domain reaches the BFF on `$PORT` (`8080`); the daemon's `3001`/`3011` listen on loopback only. |
| Login and SPA board | Pass; load time pending | Operator login (hash set on the service) and the board render in a browser after the startup-gate fix (#122). Login page assets: TTFB 0.25–0.31 s from a client to `us-west2`. A DevTools load measurement of the board was not taken. |
| Hub and worker hosts | Pass (2026-10-17) | The registry holds the `hub` entry and `worker-1`, both online. `worker-1` was added from the UI with the **private** URL `http://<service>.railway.internal:3011`, verified over IPv4 and IPv6 from the hub container. |
| SSE | Streams open; latency pending | Long-lived BFF → daemon and BFF → `worker-1` connections while the board is open. Delivery latency was not measured. |
| Terminal | Pass; typing latency pending | After #127, a `worker-1` session terminal streams command output in the browser (user-verified). Typing latency was not measured. |
| Persistence and restart | Pass, limited | The host registry, GitHub and Claude sign-ins under `/data`, and the agent CLIs in `/data/home/.npm-global` were kept across several image updates. Database contents were not compared. |
| Image update and rollback | Update: pass; rollback pending | Updates are published and deployed by `hub-image.yml` (see [Automated image publish and deploy](#automated-image-publish-and-deploy)), with a 60 s post-`SUCCESS` re-check added after a crash loop (#128). A rollback was not exercised. |
| Backup and restore | Pending human validation | Record the approved backup/snapshot and restore test; API spike did not verify it. |
| Orchestrator credentials (RW-5.4.1) | Pending human validation | `ao agent ls` shows the chosen harness `authorized`; a hub orchestrator answers a prompt and runs `gh issue view` against this repository. No credential output. |

The spike in [`packages/hub-bff/docs/railway-api-notes.md`](../../packages/hub-bff/docs/railway-api-notes.md)
confirmed only selected Railway GraphQL create/configuration operations with a
public test image. It did not deploy this hub image or verify hub startup,
private worker networking, image timing, volume persistence/backup, or deletion
permissions. Those items remain open and must not be reported as passed.

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
- A worker service that builds from this repository reads the root
  [`railway.json`](../../railway.json). Its `build.watchPatterns` list the
  worker image inputs (the paths `worker/Dockerfile.dockerignore` admits, the
  worker directory, and `railway.json` itself), so a push that changes nothing
  else does not redeploy the worker and stop its live sessions. Keep the list
  in sync with that `.dockerignore`. Config-as-code overrides the dashboard's
  watch paths; services that deploy from an image do not read the file.
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
| Railway target | Pass | Project `<project-name>` (`<project-id>`), environment `<environment-name>` (`<environment-id>`), service `<service-name>` (`<service-id>`). |
| Active deployment | Pass | Deployment `<deployment-id>` is `SUCCESS`/`RUNNING`, created `2026-10-17`, commit `<commit>` on `feat/railway-hosting`, image digest `<image-digest>`. |
| Source/build | Pass | Repository `petarnenov/ultra-factory`, Dockerfile `deploy/railway/worker/Dockerfile`, root build context (`rootDirectory=null`). |
| Volume | Pass, persistence configured | `ultra-factory-volume` is `READY`, 50 GB, mounted at `/data`. Read-only metadata showed `/data/ao/ao.db` present (2,154,496 bytes); database contents were not read. |
| Public HTTPS and identity | Pass | `https://<worker-domain>/api/v1/identity` returned HTTP 200 with TLS verification result 0, API version 1, and host ID `<host-id>`. |
| Readiness and authenticated route | Pass | On HTTPS, `/readyz` and `/api/v1/sessions` returned 401 without authentication and 200 with the local bearer credential. No credential value was output. |
| Public routing | Pass | Domain `<worker-domain>` is `ACTIVE` and targets port `3011`. |
| Main listener port `3001` | Pass, loopback-only | Read-only `/proc/net/tcp` and `/proc/net/tcp6` inspection showed `127.0.0.1:3001` and no wildcard listener on that port at inspection time. |
| Remote listener port `3011` | Pass, dual-stack | `/proc/net/tcp6` showed a wildcard listener on 3011 and `bindv6only=0`; the configured `AO_REMOTE_BIND` is `dual` and `AO_REMOTE_TRUSTED_PROXY` is `1`. |
| Private-network reachability | Pass, self-route | From the service container, `<service>.railway.internal` resolved to IPv4 and IPv6. Requests to `http://<service>.railway.internal:3011/api/v1/identity` returned 200 over both families. A separate peer service was not created, so peer-to-peer reachability remains unverified. |
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
