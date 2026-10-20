# Railway deployment runbook

Operator runbook for AO on Railway: one **hub** service (AO daemon, web BFF, and
web SPA) and one or more **worker** services (AO daemons that run agent
sessions). It consolidates the worker runbook (RW-1.4), the hub deployment
(RW-2.5), credentials and rotation (RW-4.1.2, RW-5.4.1), and backup, drain,
and limits (RW-4.2).

- How the pieces fit together and how requests flow:
  [Railway architecture](../../docs/railway/architecture.md).
- Security controls and residual risk:
  [threat model](../../docs/railway/threat-model.md).
- Image build details: [hub/Dockerfile](hub/Dockerfile),
  [worker/README.md](worker/README.md), and the build graph that ties them
  together, [docker-bake.hcl](docker-bake.hcl). BFF routes and configuration:
  [`packages/hub-bff/README.md`](../../packages/hub-bff/README.md).
- Decisions: ADR [0005 (trusted proxy)](../../docs/adr/0005-trusted-proxy-remote-host.md),
  [0005 (report pull)](../../docs/adr/0005-daemon-pulls-remote-worker-reports.md),
  [0006](../../docs/adr/0006-web-bff-for-hub.md),
  [0007](../../docs/adr/0007-dual-stack-remote-host-listener.md),
  [0008](../../docs/adr/0008-hub-egress-to-off-railway-hosts.md),
  [0010](../../docs/adr/0010-coordinator-hierarchy-and-report-ownership.md).

Conventions for everything below:

- Railway setup, deployment, and live checks are operator procedures. Agents do
  not create, redeploy, or delete Railway resources.
- Record live results with domains, Railway IDs, host IDs, and digests masked as
  in RW-1.4.1 (`<hub-domain>`, `<worker-domain>`, `<project-id>`, ...). Never
  paste a password, hash, token, bearer, `mobile/config.json`, environment dump,
  or `ao remote-host status` output into issues, logs, or evidence.
- A check that is not listed as passed in the
  [validation record](#validation-record) is **pending** and must not be
  reported as passed. Open live checks are listed under
  [Pending live checks](#pending-live-checks).
- Merging a change under `deploy/railway/` (or `backend/`, `frontend/`,
  `packages/`) to `main` rebuilds and redeploys the hub, which
  ends live hub sessions. See
  [Automated image publish and deploy](#automated-image-publish-and-deploy).

## Contents

1. [Worker service](#worker-service)
2. [Hub service](#hub-service)
3. [Credentials and rotation](#credentials-and-rotation)
4. [Backup, drain, redeploy, and rollback](#backup-drain-redeploy-and-rollback)
5. [Limits and cost monitoring](#limits-and-cost-monitoring)
6. [Coordinator rollout and rollback (pilot)](#coordinator-rollout-and-rollback-pilot)
7. [Local end-to-end test](#local-end-to-end-test)
8. [Logs and troubleshooting](#logs-and-troubleshooting)
9. [Validation record](#validation-record)

## Worker service

A worker is the image from [worker/Dockerfile](worker/Dockerfile) with the
authenticated remote-host listener on port `3011`. It is created either by the
operator (as in RW-1.4) and added to the hub by address, or by the hub through
the Railway API (see [Railway workers from the hub](#railway-workers-from-the-hub)).

### Prerequisites

- Obtain the worker's verified Railway project, environment, service, source
  revision or image digest, host ID, public HTTPS domain targeting authenticated
  port `3011`, and evidence of a persistent volume at `/data` (see the
  [worker verification log](#worker-verification-log-rw-141)). Recheck the
  deployment before pairing because service state can change.
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
- For pairing from the desktop app: a desktop app and CLI compatible with the
  deployed worker revision. The desktop app is the recommended installation
  path. Enable Developer mode under Settings → General and sign in to AO Cloud
  to use Settings → Remote hosts, as described in
  [self-hosted remote hosts](../../docs/self-hosted-remote.md). The web UI on
  the hub needs neither.
- An operator-approved test repository, Git identity, permission to push a test
  branch and create a PR, and that project's build dependencies on the worker.
  Agent and GitHub authentication belong on the worker; laptop or hub
  credentials are not forwarded. Supply secrets privately through the service
  configuration.

Before pairing, verify public routing to `3011` (or private reachability from
the hub), rejection of unauthenticated protected API requests, and that `3001`
is not published. `GET /api/v1/identity` is the exact unauthenticated
exception; compare its host ID with the worker's locally reported identity.
Record private IPv4 and IPv6 connectivity separately. Repository
implementation and configuration claims do not prove Railway reachability. Do
not expose the loopback listener to fix it.

### Worker environment contract

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

### Pair from the desktop app (human required; not run)

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

### CLI repeat (human required; not run)

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

## Hub service

The hub image from [hub/Dockerfile](hub/Dockerfile) runs the AO daemon, the hub
BFF, and the web SPA in one Railway service. The first live deployment and its
sanitized results are recorded in the
[hub validation record](#hub-validation-record-rw-253).

### Create the service

1. Build and publish the hub image from the repository root using the
   instructions in [hub/Dockerfile](hub/Dockerfile), or select the published
   hub image tag (normally published by
   [`hub-image.yml`](#automated-image-publish-and-deploy)). Do not put
   credentials in build arguments or the image.
2. Create a Railway service named `hub` from that image. Leave the start command
   empty so the image entrypoint runs. Attach one persistent volume at `/data`.
   The supervisor expects to initialize the volume as root and then run as uid
   `10001`; set `RAILWAY_RUN_UID=0` so it can prepare ownership and permissions.
3. Railway sets `PORT`; the BFF listens on it (the image default is `8080`).
   Configure the service's public domain to target `$PORT`, never daemon ports
   `3001` or `3011`. A Railway-generated domain with TLS was verified on
   2026-10-17; custom domain DNS/TLS is not verified.
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
   the salted scrypt hash.
5. Sign in at `https://<hub-domain>/login` and confirm the board loads and
   `/hub/hosts` lists the `hub` entry as online.

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
| `AO_HUB_WORKER_URL_MODE` | Optional `public` or `private`; default `public`. Hub → worker private DNS was verified for a manually added worker (2026-10-17); provisioning in `private` mode is not live-verified. |
| `AO_HARNESS_UPDATE_CHECKS` | Image default `on` for the hub (the worker image keeps `off`), so Settings → Harness → This computer reports updates and offers Update. |
| `NPM_CONFIG_PREFIX`, `AO_HUB_NPM_SEED` | Image defaults `/data/home/.npm-global` and `/opt/ao/npm-global`. The image installs Claude Code and Codex under the seed; the supervisor copies them into the volume prefix when it lacks them or has an older version. `PATH` searches the volume prefix first, so a harness update from the UI installs there and persists across restarts and image updates. |
| `AO_HUB_OPENCODE_V2_SEED` | Image default `/opt/ao/opencode-v2-npm`. The image installs OpenCode 2 (`@opencode/cli`) under this seed; the supervisor copies it into the daemon's private OpenCode 2 npm prefix (`$HOME/.local/share/opencode-v2-home/npm`) when the volume lacks it or has an older version, so it never replaces the worker image's OpenCode 1. Updates from Settings → Harness install into that same prefix and persist across restarts and image updates. |
| `ANTHROPIC_API_KEY` | Optional. Claude Code credential for an orchestrator in the hub, read by `claude` from the environment. See [Orchestrator agent credentials](#orchestrator-agent-credentials). |
| `CLAUDE_CODE_OAUTH_TOKEN` | Optional alternative to `ANTHROPIC_API_KEY` for Claude Code. Set at most one of the two. |
| `OPENAI_API_KEY` | Optional. Codex API key; used only for the one-time `codex login --with-api-key` below, not read by AO's readiness probe. |
| `AO_HUB_ORCHESTRATOR_TICK` | Optional. `1` starts the orchestrator wake-up loop (`ao orchestrator tick loop`) under the supervisor; default off. It is configured by the `RW_TICK_*` variables; see [Orchestrator wake-up loop](#orchestrator-wake-up-loop). |
| `AO_SUPERVISED` | Set by the image supervisor to `1` for the daemon. The daemon then refuses loopback `POST /shutdown`, so `ao stop` cannot take the hub down from an agent session; restart the service from Railway instead. |
| `TS_AUTHKEY` | Optional. A pre-approved, non-reusable, non-ephemeral auth key tagged `tag:ao-hub`. When present, the supervisor joins the tailnet in userspace mode and passes `AO_REMOTE_TAILNET_SOCKS=127.0.0.1:1055` only to the daemon, the BFF and the orchestrator tick loop (whose `ao session ls --host` calls reach the remote hosts) after `tailscale up` succeeds. The key is read from the supervisor environment, sent to `tailscale up` over stdin, and is not logged, passed in a child environment or argv, or written to `/data`. |
| `TS_HOSTNAME` | Optional Tailscale node name; defaults to `ao-hub`. |
| `TS_DEBUG_MTU` | Optional Tailscale tunnel MTU for the hub; defaults to `1232`. Railway's `railnet0` has MTU 1316, and Tailscale's default 1280 makes full-size WireGuard packets 1340 bytes, so large responses from some tailnet hosts are lost (`ao session ls --host` times out while `ao host ls` shows them online). |
| `GH_TOKEN` | Required for an orchestrator in the hub. Fine-grained token limited to this repository; read by `gh` and, through `gh auth token`, by the daemon's GitHub adapters. |

**Tailnet key rotation.** Create a new pre-approved key with the same `tag:ao-hub`,
non-reusable and non-ephemeral settings. Replace `TS_AUTHKEY` in Railway and
redeploy; the node state under `/data/tailscale` keeps the existing node identity.
Confirm the hub is online in the tailnet with `tag:ao-hub`, then revoke the old
key in the Tailscale admin console. If the volume or node state is lost, remove
the old node entry after the replacement joins. Never put an auth key in a shell
command, support log, issue, or screenshot.

`AO_HUB_OPERATOR_PASSWORD_HASH`, `AO_HUB_SESSION_SECRET`, and
`AO_HUB_DAEMON_URL` from the earlier task text are not read by the current
code. The operator hash is `AO_WEB_PASSWORD_HASH`; the hub entry uses the
daemon password file and its `lastPort` (normally `3011`) to form the internal
loopback URL. As specified by [ADR 0006](../../docs/adr/0006-web-bff-for-hub.md),
sessions are opaque random IDs held in BFF memory; there is no signing secret.
A BFF redeploy clears all sessions by design.

### Data layout

The hub image places durable state under the mounted volume:

| Path | Contents |
| --- | --- |
| `/data/ao` | Daemon database, worktrees, `running.json`, and `mobile/config.json` for the loopback-only listener credential. |
| `/data/hub/hosts.json` | Host registry, including worker connection credentials; stored with restrictive permissions. |
| `/data/hub/audit.jsonl` | Append-only security events; mode `0600`, no secrets. |
| `/data/home` | Hub home directory, including any interactive agent logins (`.claude/.credentials.json`, `.codex/auth.json`), the agent CLI prefix `.npm-global`, and the private OpenCode 2 npm prefix `.local/share/opencode-v2-home/npm`. |
| `/data/tailscale` | Tailscale node state when `TS_AUTHKEY` is set; directory mode `0700`, owned by uid `10001`. It contains the durable node identity, never the auth key. |

### Add a worker by address

After the hub is deployed and login works, open **Settings → Remote hosts** in
the hub UI and add the worker with label `worker-1`, its listener URL, and its
pairing password. The private URL `http://<service>.railway.internal:3011` was
verified from the hub over IPv4 and IPv6 on 2026-10-17; recheck it for a
worker in another project or environment, because Railway private networking
only reaches services in the same project. If private networking does not work,
use the worker's public HTTPS URL and verify that its public domain targets the
authenticated listener on `3011`; never expose the hub daemon's `3001`
listener. Confirm `/hub/hosts` contains the hub entry and the worker entry, and
that the worker reports online. Do not include pairing credentials in
screenshots or logs.

The BFF probes the identity route first, then checks the bearer with an
authenticated read, and saves the host only if both pass. Removing a manual host
deletes only the registry record; nothing in Railway changes.

### Railway workers from the hub

With `RAILWAY_API_TOKEN`, `RAILWAY_PROJECT_ID`, `RAILWAY_ENVIRONMENT_ID`, and
`AO_HUB_WORKER_IMAGE` set, Settings → Workers creates and removes
`ao-worker-<id>` services through the Railway API (see the
[architecture](../../docs/railway/architecture.md#railway-api-worker-management)).
Keep these variables unset until an operator has verified, with an authorized
project-scoped token, that the token can delete services and volumes: the token
tested in RW-3.1.1 could not. Never substitute an account-wide token; the BFF
refuses to start with one. Removing a Railway worker deletes its service and its
volume, so never use **Remove** as a redeploy.

### Orchestration roles and the review flow

The user agreed this process on 2026-10-19. It decides where work may run.

- **The hub runs only the orchestrator.** The hub service runs the
  orchestrator session with the daemon, the BFF and the wake-up loop. Do not
  start implementation, fixes, tests or reviews on the hub. It is not a build
  machine: it has no Go toolchain, no Docker, no python3 and no actionlint, so
  a Go change cannot be validated there. Start work on a worker machine with
  `ao spawn --host`.
- **Workers report to their orchestrator.** A worker sends its reports to the
  orchestrator that spawned it (ADR 0005, pull). The orchestrator does not wait
  for a report to inspect state; `ao session ls --host`, `gh pr list --base dev`
  and `gh pr checks` are authoritative.
- **Reviews run where the worker's checkout lives.** AO's reviewer is invoked
  once over the worker's checkout with an enforced read-only guarantee, so it
  is started on the machine that owns the worker. `ao review trigger|ls|cancel
  --host` runs a review on that machine from another orchestrator. Do not start
  a review-only worker on the hub as a substitute.
- **Preconditions before a review.** The worker is idle or exited; everything
  is committed and pushed; the pull request is open; and the reviewer is given
  the evaluated head SHA. These protect against reviewing a tree that is not
  the pull request.
- **After a review.** Send the findings to the owning worker; it fixes and
  pushes. A moved head needs a new review pass.
- **Agent configuration is per machine.** Project Settings / Agents and the
  machine's harness defaults apply on the machine that runs the session. The
  precedence is defined in #254; the hub's configuration is not copied to a
  worker.
- **A machine cannot be both an orchestrator and a worker host.** A machine
  that runs its own orchestrator refuses a worker started from another machine
  with `LOCAL_ORCHESTRATOR_ACTIVE`, because the local orchestrator would claim
  that worker's reports. This happened on `IMac`, whose local orchestrator
  `geowealth-1` was active, and the spawn was refused. Before
  `ao spawn --host <H>`, check `ao orchestrator ls --host <H>`. If the machine
  is needed as a worker while it runs an orchestrator for another project,
  stop that orchestrator first or use another worker machine.

The orchestrator instructions that apply these rules are in
[`docs/backlog/railway/orchestrator-prompt.md`](../../docs/backlog/railway/orchestrator-prompt.md).

#### CI gate for worker pull requests

The project setting `ciGate` makes workers open draft pull requests so the
expensive CI waits for AO review. It is off by default.

```bash
# On the machine that runs the project (a remote worker reads its own host's
# project config, the same as autoReview).
ao project set-config <project-id> --config-json '{"autoReview":true,"ciGate":{"mode":"review","withoutAutoReview":"ready"}}'
```

`set-config` replaces the whole config, so start from `ao project get <id>
--json` and add the `ciGate` block to it.

- `mode`: `off` (default) or `review`.
- `withoutAutoReview`: `ready` (default) or `manual`.
- **`mode=review` only has an effect for a session with auto-review on, or
  when `withoutAutoReview=manual`.** A session without auto-review and with
  `withoutAutoReview=ready` opens a normal pull request, so nothing is left
  stuck as a draft. With `manual`, the pull request stays a draft until an
  `approved` verdict from a manual `ao review trigger`, or `ao pr ready`.
- The value is resolved when a session is spawned and shown as `ciGateActive`
  on the session, so a change affects only new sessions.
- **Manual override.** `ao pr ready [<session>] [--undo]` marks the session's
  pull request ready or back to draft; `--host` runs it on the machine that
  owns the session. The root orchestrator and the owning coordinator may run
  it; a worker is refused with `CI_GATE_FORBIDDEN`. The command needs the
  daemon's GitHub account to be a user or app token, not `GITHUB_TOKEN`,
  because the draft flip uses the GraphQL API. On a non-GitHub provider, or a
  daemon without GitHub credentials, it fails with `CI_GATE_UNSUPPORTED`. Each
  flip is recorded with the
  caller, the pull request and its head commit.

### Orchestrator agent credentials

An orchestrator started in the hub runs one of its agent CLIs: `claude` and
`codex` from the hub's updatable prefix (see `NPM_CONFIG_PREFIX` above),
`opencode` (OpenCode 1) inherited from the worker image, or OpenCode 2 from the
private prefix the hub seeds (see `AO_HUB_OPENCODE_V2_SEED` above). It needs a
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

**Live check (operator, passed 2026-10-17, #134).** In a hub terminal, `ao agent ls`
lists the chosen harness as `authorized`; an orchestrator session started in
the hub answers a prompt and runs
`gh issue view 119 --repo petarnenov/ultra-factory`. Record only the result
and the date, never `gh auth status -t` output or environment dumps.

### Orchestrator wake-up loop

Worker reports reach a hub orchestrator through the ADR 0005 pull, but no
report covers PR and CI state (a PR that turns green, a merge conflict). The
wake-up loop covers it: `ao orchestrator tick loop` runs as a third child of
the supervisor when the `hub` service has `AO_HUB_ORCHESTRATOR_TICK=1`. It is
part of the `ao` binary (package `backend/internal/orchestratortick`), so it
ships, is versioned and is tested with the rest of AO.

- **What one tick does.** It finds the single live orchestrator of
  `RW_TICK_PROJECT` (default `ultra-factory`), or the session in
  `RW_TICK_SESSION`. Zero or several live orchestrators, or a daemon that is
  not ready, are logged as errors and nothing is sent. An orchestrator in an
  active turn is never interrupted. Otherwise it reads, concurrently, the
  `rw/` worker sessions of `RW_TICK_HOSTS` (default `Linux IMac Z600`) and, in
  one GitHub GraphQL query, the open and merged PRs to `dev`
  with their checks, the open `rw` issues, and how far the branch is behind
  `main`. It checks five invariants (branch contains `main`, no green
  mergeable PR left open, no merged PR with an open task issue, no idle
  worker without a PR, the claude-code + codex worker mix) and sends one
  `TICK` message when the state changed, or when nothing changed for 30
  minutes while work is in flight (10 with a violation). A violation that only
  grows older ("green for 35 min") is the same state, not a change.
- **GitHub.** Read with `AO_GITHUB_TOKEN`, `GH_TOKEN`, `GITHUB_TOKEN` or
  `gh auth token`, in that order (see
  [Orchestrator agent credentials](#orchestrator-agent-credentials)). A missing
  or rejected token is reported to the orchestrator as a violation.
- **Configuration.** `RW_TICK_*` variables: `PROJECT`, `SESSION`, `HOSTS`,
  `REPO`, `BASE`, `MAIN`, `ISSUE_LABEL`, `BRANCH_PREFIX`, `INTERVAL` and
  `TIMEOUT` (seconds), `RENUDGE_MINUTES`, `VIOLATION_RENUDGE_MINUTES`,
  `GREEN_PR_MAX_MINUTES`, `IDLE_WORKER_MAX_MINUTES`, `AGENT_MIX_SINCE`
  (RFC 3339), `PAUSED`, `HOLD_LABELS`, `MERGE_AUTHORIZED`, `CLOSE_COMPLETED`.
  `RW_TICK_INTERVAL` defaults to 180 seconds (3 minutes); an explicit value
  overrides it. An invalid value stops the command with an error.
- **Completion reconciliation.** Two explicit durable opt-ins extend the same
  cadence. `RW_TICK_MERGE_AUTHORIZED=1` permits a normal squash merge only when
  the fresh provider observation proves the exact current head has passing
  required checks, a current-head AO native review verdict of `up_to_date` for
  the exact head, `MERGEABLE`, no unresolved review threads, and no hold label
  (`RW_TICK_HOLD_LABELS`, default `needs-human do-not-merge status:blocked`).
  A GitHub `APPROVED` rollup or an approval comment is not a substitute for
  the native verdict; provider `CHANGES_REQUESTED`/`REVIEW_REQUIRED` still
  block. The merge request carries the evaluated head SHA, so a head change
  between the check and the mutation is refused rather than merged. Without
  the flag the exact missing authorization is reported in the tick message
  and `status`.
  `RW_TICK_CLOSE_COMPLETED=1` permits terminating a worker after at least one
  owned PR merged and every owned PR is terminal, the agent is not active, the
  branch push state is known and has no unpublished commits, and no durable
  obligation remains. The lifecycle kill preserves worktrees, branches, and
  artifacts; it never force-cleans. A worker that must deploy or verify live
  stays open through an obligation recorded with
  `ao orchestrator tick hold <session> --host <H> --reason <why> --until
  <condition>`, and `ao orchestrator tick release <session> --host <H>` clears
  it. Merge and close actions are recorded in
  `$AO_DATA_DIR/orchestrator-tick/completion.json`; completed actions are not
  replayed, and a failed close or merge remains actionable for the next tick.
  Pause is checked before any action, so a stopped campaign never merges,
  closes, or restarts work from an older queued tick.
- **Logs and state.** Each tick writes one structured (logfmt) line to stderr,
  which is the service log; violations are logged as warnings. State lives in
  `$AO_DATA_DIR/orchestrator-tick/` (`/data/ao/orchestrator-tick/`, mode
  0700): the maintenance flag, the last tick's outcome, and the fingerprint of
  the last state sent. Files are replaced atomically. The `TICK` message itself
  is not logged.
- **Start and failure.** The first tick runs 60 seconds after the container
  starts (`AO_HUB_TICK_START_DELAY`). Each tick is bounded by
  `RW_TICK_TIMEOUT` (default 240 s); a failed or panicking tick is logged and
  the loop continues. SIGTERM stops the loop at once. If the process exits,
  the supervisor restarts it after 60 seconds; the daemon and the BFF keep
  running.

**Inspect and pause.** In a hub terminal (uid `10001`, `HOME=/data/home`):

```bash
ao orchestrator tick status             # maintenance flag, last tick, last message sent
ao orchestrator tick snapshot           # the state a tick would report; sends nothing
ao orchestrator tick run --dry-run      # a whole tick that prints the message instead of sending it
ao orchestrator tick pause "redeploy"   # sets the maintenance flag
ao orchestrator tick resume             # clears it
ao orchestrator tick hold uf-41 --host Linux --reason "deployment pending" --until "live check passes"
ao orchestrator tick release uf-41 --host Linux
```

The flag is on the volume, so it survives a redeploy; resume after the hub is
verified. While it is set every tick logs `outcome=paused` and sends nothing.
Setting `RW_TICK_PAUSED=1` on the service pauses as well, but changing a
variable restarts the hub. To turn the loop off for good, remove
`AO_HUB_ORCHESTRATOR_TICK`. Run only one loop per orchestrator.

**Tests.** `go test ./internal/orchestratortick/ ./internal/cli/ -run Tick`
(from `backend/`) covers the invariants, the send decision, pause, the loop's
shutdown and panic recovery, and the GitHub and daemon adapters.

### Cutover to the hub orchestrator

1. Enrol each worker machine in the tailnet as `tag:ao-host`, with the
   least-privilege ACL from [ADR 0008](../../docs/adr/0008-hub-egress-to-off-railway-hosts.md),
   and verify its authenticated listener from the hub without opening a home
   router or firewall port.
2. Add each host to the hub by its tailnet IPv4, IPv6, or MagicDNS address and
   confirm it is online with `ao host ls` in the hub.
3. Stop the Mac orchestrator from creating workers. Wait for its existing
   remote workers to finish and for every remote report link to be removed.
4. Stop the Mac orchestrator and any wake-up loop that targets it.
5. Start the hub orchestrator, set `AO_HUB_ORCHESTRATOR_TICK=1`, and verify that
   only the hub orchestrator and tick are active. Before every planned hub
   redeploy, pause the tick as described above and resume it only after the hub
   checks pass.

### Rollback to the Mac orchestrator

**Status (2026-10-17): documented, not yet tried.** Pause the hub tick, stop the
hub orchestrator, and remove `AO_HUB_ORCHESTRATOR_TICK` if the hub must remain
without a wake-up loop. Restart the Mac orchestrator with its orchestrator
prompt, then run `ao orchestrator tick loop` on the Mac under a process
supervisor (launchd or systemd) with the same `RW_TICK_*` settings. Verify
that exactly one orchestrator and one loop are active before allowing new work.

### Updating the hub's Tailscale version

Update `TAILSCALE_VERSION`, `TAILSCALE_SHA256_AMD64`, and
`TAILSCALE_SHA256_ARM64` together in `hub/Dockerfile`. Download both archives
from the [stable package server](https://pkgs.tailscale.com/stable/), compute
their hashes locally, and cross-check the published `.sha256` files:

```bash
version=1.104.1 # Replace with the new version.
download_dir=$(mktemp -d)
for arch in amd64 arm64; do
  archive="tailscale_${version}_${arch}.tgz"
  curl -fLsS "https://pkgs.tailscale.com/stable/${archive}" -o "$download_dir/$archive"
  curl -fLsS "https://pkgs.tailscale.com/stable/${archive}.sha256" -o "$download_dir/$archive.sha256"
  sha256sum "$download_dir/$archive"
  printf '%s  %s\n' "$(cat "$download_dir/$archive.sha256")" "$download_dir/$archive" | sha256sum --check
done
```

Copy each locally computed hash into the matching architecture's build arg.
The build checks the pinned hash first, then the published hash, before
extracting the archive. Rebuild the hub for `linux/amd64` and `linux/arm64`
when bumping the version. From the repository root, build a base image and run
this negative check, which must fail at archive verification:

```bash
docker build -f deploy/railway/worker/Dockerfile -t ao-worker:dev .
docker buildx build --platform linux/amd64 -f deploy/railway/hub/Dockerfile \
  --build-context ao-worker=docker-image://ao-worker:dev \
  --build-arg TAILSCALE_SHA256_AMD64=0000000000000000000000000000000000000000000000000000000000000000 \
  -t ao-hub:checksum-negative .
```

### Automated image publish and deploy

[`.github/workflows/hub-image.yml`](../../.github/workflows/hub-image.yml)
builds both images from the pushed commit in one build graph and smoke-tests
the hub, on every push to `main` that touches its inputs
(`backend/`, `frontend/`, `packages/`, `deploy/railway/`, or the workflow).
The base is the worker target from that same commit
([docker-bake.hcl](docker-bake.hcl)), not a published `ao-worker` tag, so the
hub's daemon and tools can never lag the commit it was built from. It
publishes `ghcr.io/<owner>/ao-hub:sha-<short>` for linux/amd64, the platform
Railway runs, then the deploy job sets the Railway `hub` service image to that
tag and waits for the deployment to reach `SUCCESS`, with a 60 s re-check
after `SUCCESS` to catch a crash loop. Only pushes to `main` publish and
deploy; pull requests and manual runs smoke-test without publishing. Every
deploy restarts the hub, as in
[Hub image update](#hub-image-update). To approve each deploy instead, add
required reviewers to the `railway-hub` environment.

One-time setup (operator):

1. In each GHCR package's settings (`ao-hub`, `ao-worker`), under **Manage
   Actions access**, give this repository write access. Packages first pushed
   from a laptop are not linked to the repository, and `GITHUB_TOKEN` cannot
   push them otherwise.
2. Keep `ao-hub` pullable by Railway: public, or registry credentials on the
   service.
3. Create a Railway project token for the hub's project and environment
   (Project Settings → Tokens). In GitHub, create the `railway-hub`
   environment with the secrets `RAILWAY_TOKEN` (that token) and
   `RAILWAY_HUB_SERVICE_ID` (the `hub` service ID). Without them the deploy
   job is skipped with a notice and the image is still published.

The deploy uses Railway's `serviceInstanceUpdate`, `serviceInstanceDeployV2`,
and `deployment` GraphQL operations with the project token's
`Project-Access-Token` header.

## Credentials and rotation

Credential inventory and why each matters: [threat model](../../docs/railway/threat-model.md#assets).
A credential that may have leaked is revoked first and replaced second.

- **Operator password:** generate a new hash with the command in
  [Create the service](#create-the-service), replace `AO_WEB_PASSWORD_HASH` in
  Railway, then redeploy the BFF. Replacing the operator password and
  redeploying invalidates all sessions because sessions are opaque IDs held in
  memory; this is the intended behavior. Verify the new password works and the
  old password fails. Do not change `AO_WEB_PASSWORD` or use a plaintext Railway
  variable. There is no session signing secret to configure or rotate. To sign
  out every browser without a password change, use logout everywhere
  (`DELETE /hub/sessions`).
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
  token. The CI deploy token (`RAILWAY_TOKEN` in the `railway-hub` GitHub
  environment) is a separate project token; rotate it the same way.
- **Agent and GitHub credentials (hub):** for Railway variables, create the new
  key or token, replace the variable on the `hub` service, redeploy when no hub
  session is active (see [Hub image update](#hub-image-update): a restart ends
  live agents), confirm `ao agent ls` and `gh auth status` in the hub, then
  revoke the old credential at the provider. For interactive logins, log in
  again in a hub terminal (for Codex, `codex logout` first), confirm
  `ao agent ls`, then revoke the old session or key at the provider. Worker
  agent and GitHub credentials are service variables on each worker; rotate them
  the same way, after [draining the worker](#worker-drain-procedure).
- **Hub daemon credential:** keep `/data/ao/mobile/config.json` with the volume;
  the supervisor seeds the listener as loopback-only on port `3011` and retains
  the generated password. Never publish either daemon port. The BFF rereads the
  hub entry before every poll, but credential rotation behavior for that entry
  should be separately validated before changing the file.
- **Worker password:** see [Worker password rotation](#worker-password-rotation).

### Worker password rotation

Operator only; not run. Schedule a maintenance window and retain private Railway
console access. The CLI has `remote-host status`, `enable`, and `disable`; it
has no password rotation subcommand. Merely changing `AO_REMOTE_PASSWORD` on an
initialized volume has no effect: the worker entrypoint ignores that variable
after first boot. Do not delete `mobile/config.json` to rotate it.

The daemon provides `POST /api/v1/mobile/regenerate` on its loopback API at
`127.0.0.1:3001`. The authenticated remote-host listener and the BFF proxy
intentionally block `/api/v1/mobile`, so
`/hosts/:id/api/v1/mobile/regenerate` cannot be used. Inside the selected
worker's Railway SSH shell, call the loopback route:

```bash
curl --fail --silent --show-error -X POST http://127.0.0.1:3001/api/v1/mobile/regenerate
```

The response contains the new password: keep it private. Rotation revokes the
account credential. Then update every client that stores the old password:

- **Hub registry.** The BFF has no route to update a host credential, so update
  only the matching worker's `password` field in `/data/hub/hosts.json` on the
  hub volume using an atomic replacement that preserves mode `0600`, then
  restart the BFF and probe the host.
- **Desktop app.** Update the saved desktop connection password and re-pair the
  AO account as needed.
- **Account token.** If `AO_REMOTE_ACCOUNT_TOKEN` is configured, rotate or
  remove that variable as well: otherwise the entrypoint reinstates its old hash
  on next start.

Privately verify that old credentials fail and the new credential works, then
repeat after restart. Do not put either value in logs, shell history, or issue
evidence.

### Audit log

`/data/hub/audit.jsonl` is an append-only JSONL file with mode `0600`. It
records login success and failure, host add and remove, worker create and
delete, forced worker deletion, and entering and leaving worker drain. Entries
hold only a timestamp, an event name, and an optional opaque host or worker ID.
Read the newest entries from the UI or with `GET /hub/audit?limit=` (default
`100`, maximum `1000`).

## Backup, drain, redeploy, and rollback

A container restart ends live agent processes, tmux/PTY processes, terminals,
SSE connections, preview servers, and BFF sessions. Only what is on `/data`
survives, and only when the same volume is mounted again. Persisted session
records do not mean that live sessions resume.

### Hub database and registry backup

`deploy/railway/scripts/backup-volume.sh` exports the hub's `/data/ao/ao.db`
using SQLite `.backup`, checks the snapshot's integrity, and archives it with
`/data/hub/hosts.json`. It requires the image's `sqlite3`, `tar`, and `base64`,
and local Bash, Python 3, and an authenticated Railway CLI. It requires explicit
project, environment, and service arguments; it never uses the linked default.

```bash
# Safe inspection only: no SSH, local archive, or Railway mutation.
deploy/railway/scripts/backup-volume.sh --dry-run PROJECT ENVIRONMENT HUB_SERVICE ./hub-backup.tar.gz
# Operator-only live export, outside agent verification:
deploy/railway/scripts/backup-volume.sh PROJECT ENVIRONMENT HUB_SERVICE ./hub-backup.tar.gz
```

The local archive is mode 0600 and an existing destination is never overwritten.
Treat it as a credential: `hosts.json` contains worker bearers. Keep it on encrypted
storage with restricted access and a retention policy. The script supports local
export; upload to an S3-compatible destination separately using your approved
credentials, encryption, and retention settings. No bucket credentials are stored
in the script or archive. Schedule backups before maintenance and at the cadence
required by your recovery-point objective, and test restoration periodically.

This is a database/registry backup, **not a full volume backup**. It does not contain
worktrees, agent transcripts, HOME, mobile configuration, audit logs, or other
credentials. Preserve those through a separate volume backup before destructive
maintenance. It targets the hub: a worker without `hosts.json` fails closed.
Database and registry snapshots are individually consistent, not a cross-service
transaction; pause registry changes during export. Interrupted SSH or invalid
archives leave no published local backup. A hard container kill may leave a
private `backup.*` staging directory on the volume; remove it during maintenance.

### Volume backup and restore

Before a volume backup, stop or quiesce the service so SQLite and registry
writes are not in flight, then use the operator-approved Railway volume
backup/snapshot procedure and record its timestamp and volume identity. Volume
snapshots contain every bearer and interactive login on that volume; protect
them like `~/.ao`. The exact Railway backup/restore mechanism and volume
recovery behavior are pending operator validation (#145).

For a restore rehearsal, use an isolated, stopped service and a copy of its volume.
Inspect the archive member list (`ao.db`, `hosts.json`) before extraction. Run
`sqlite3 ao.db 'PRAGMA integrity_check;'` and verify expected session records.
Stop both daemon and BFF before replacing either file, retain the original volume,
and ensure no old database `-wal` or `-shm` files accompany the restored snapshot.
Restore file ownership for uid 10001 and mode 0600. Restore worktrees, transcripts,
and mobile configuration from the matching volume backup; do not assume this
archive alone can resume an agent. Start the isolated service, verify identity,
file ownership and permissions, login, host registry, daemon readiness, and
worker reachability, then confirm a representative session can resume before
reopening traffic.

### Worker drain procedure

Use the authenticated BFF with the normal operator session cookie and matching
`Origin` header. These routes are not daemon or Railway APIs:

- `POST /hub/workers/:id/drain` persists `draining` and returns the worker,
  sanitized non-terminated sessions, and `readyForRedeploy`. Repeating the POST
  refreshes the session check. Unknown/non-Railway workers return 404; workers
  without a completed identity/resource registration return 409.
- An unreachable or malformed session response returns 409,
  `session_check_failed`, and `readyForRedeploy: false`; the worker stays draining.
  Idle and waiting-for-input sessions still count. A failed check is never zero.
- The task composer disables new submission while draining. The BFF also refuses
  session creation, orchestrator creation/delegation, task preparation, restore,
  and resume requests to that worker with 409 `worker_draining`, including stale
  browser clients. Existing session inspection, conversation, and terminal
  connections remain available so work can finish.
- Drain survives health probes and BFF restarts. It never kills a session, deletes
  a volume, or invokes Railway redeploy. Both entering and leaving drain are audited.
- `DELETE /hub/workers/:id/drain` explicitly releases the worker after maintenance
  or cancellation. Its status returns to `unknown` until the next health probe;
  release does not claim that the worker is healthy.

Before draining, pause orchestrator delegation, scheduled spawning, and direct
CLI/desktop clients targeting that worker. The BFF gate only covers requests
through this hub; it is not a daemon-wide scheduling lock. Already accepted
requests can finish creating sessions, so wait for in-flight launches to settle
and repeat the drain check immediately before maintenance. Only an operator may
redeploy after a successful empty-session check and a verified backup. Keep the
worker drained until identity, health, persisted data, and a controlled resume
check have been verified. Never use Remove worker as a redeploy operation: removal
deletes the service and volume. Drain applies to Railway workers registered by
the hub; for a manually added worker, stop new work on it by hand.

### Worker redeploy

Operator only; not run. Drain the worker (above), stop app preview processes and
record pending work before the operator redeploys the selected service. Use the
same `/data` volume and explicitly chosen image revision; never delete the
volume as part of this test. An authorized operator may use `railway redeploy`
after selecting the correct project, environment and service; this runbook has
not executed that command.

The image places the DB, worktrees, remote-host configuration and home files on
`/data`. Their persistence depends on that same volume being mounted again.
Environment credentials must still be configured on the service. Files outside
the volume are not covered by this persistence contract.

After redeploy, verify identity and pairing, project/worktree files, DB-backed
session history, Git identity and agent authentication. Then release the drain,
start a new session and repeat terminal/message/preview checks. Record what
survived and what had to restart.

### Hub image update

Merges to `main` that touch the hub image inputs deploy
automatically ([Automated image publish and deploy](#automated-image-publish-and-deploy)).
Coordinate a hub maintenance window and pause hub work before merging such a
PR; worker drain does not protect sessions executing on the hub itself.

For a planned update: confirm no active hub sessions, stop new work, and record
pending work before redeploying. With the wake-up loop on, pause it first and
resume it after the checks below (see
[Orchestrator wake-up loop](#orchestrator-wake-up-loop)). Select the intended
image revision and reattach the same `/data` volume; after startup, verify
`/healthz`, login, the board, `/hub/hosts`, and a new terminal session. Do not
infer recovery of active work from persisted session records.

### Hub rollback

If the new image fails these checks, select the previously recorded working
image revision (`ghcr.io/<owner>/ao-hub:sha-<short>`) and redeploy it with the
same `/data` volume and compatible service variables. Verify health, login,
host entries, and a new terminal before reopening normal use. Do not roll back
by deleting or replacing the volume. This procedure is pending a human
deployment exercise (#145).

### Restart behavior and resume

A container replacement kills its PTY/tmux/provider processes; a daemon-only
restart may reconnect to surviving processes. Startup recovery is observational:
`session_manager/startup_health.go` checks the existing controller/runtime and
records an exited agent when the host is confirmed absent. Probe errors preserve
activity and make status readiness unavailable, rather than proving termination.
The periodic runtime reaper can subsequently terminate a confirmed-dead runtime,
subject to recent-activity and mass-death safeguards. The displayed board status
is derived from these facts and PR state, so it need not immediately read
"terminated" after a container restart. See [architecture](../../docs/architecture.md#runtime-reaper).

Recovery does not automatically replay every interrupted task. Resume requires
persisted provider conversation data, worktree state, and valid agent credentials.
`resume-agent` is for a live session whose agent exited; a terminated session uses
Restore. Native resume is preferred when the adapter supports it; some harnesses
can fall back to the saved prompt, while unsupported transcript-preserving restores
fail. Shutdown-saved sessions have a separate `RestoreAll` path. Check the actual
resume result; retained database rows alone do not guarantee continuation.

Local checks for this section (no Railway service or volume is contacted):

```bash
bash -n deploy/railway/scripts/backup-volume.sh
python3 deploy/railway/scripts/backup-volume_test.py
npm --prefix packages/hub-bff run typecheck
npm --prefix packages/hub-bff test
make check
cd backend && go test ./internal/observe/reaper ./internal/lifecycle ./internal/session_manager
```

## Limits and cost monitoring

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

### Read Railway usage for the project

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

### Per-process resource baseline

The 23.84 GB service-level maximum against a 24 GB limit in the CO-1.1.1
backlog is **unverified**: no committed sample attributes it to a process. The
capture protocol for a six-worker window, the current-topology workload
definition, and the comparison template are in
[resource-baseline.md](resource-baseline.md). It is read-only, needs no
deployment or configuration change, and its remaining step is one operator run
with Railway access. The capture helper is
[`scripts/railway/resource-sample.mjs`](../../scripts/railway/resource-sample.mjs).

### Recommended sizes

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

## Coordinator rollout and rollback (pilot)

The coordinator hierarchy adds one root orchestrator, up to two helper
coordinators, and up to three owned workers per coordinator. The design is
[ADR 0010](../../docs/adr/0010-coordinator-hierarchy-and-report-ownership.md)
and the root-to-coordinator rules are the
[coordinator operating contract](../../docs/coordinators/operating-contract.md).
This section is the operator procedure for the Railway topology. Statements
marked **(guidance)** are operating practice added by this runbook; they are not
ADR rules.

**Status: pilot only, not approved for regular work.** The implementation, the
end-to-end pilot, and the ADR section 8 project gate are merged on `dev`. What
remains is the operator's live run: the multi-machine pilot and its resource
comparison against the CO-1.1.1 baseline are pending on
[#227](https://github.com/petarnenov/ultra-factory/issues/227) and
[#218](https://github.com/petarnenov/ultra-factory/issues/218). Values only that
run can produce are marked **Pending operator run** below.

The gate is live and defaults to off: no project spawns a coordinator until its
`coordinators.enabled` is set (see
[Enable one pilot project](#enable-one-pilot-project)). Keep feature and config
defaults disabled until the pilot is approved. **(guidance)** Record the
approval as a dated decision with a named rollback owner; the existence of this
runbook is not approval.

### Implementation status and evidence

| Part | Where |
| --- | --- |
| Decision: coordinator kind, durable ownership, routing, capacity, recovery | [ADR 0010](../../docs/adr/0010-coordinator-hierarchy-and-report-ownership.md) |
| Root and coordinator operating rules | [Coordinator operating contract](../../docs/coordinators/operating-contract.md) |
| Durable ownership and audit | #220, #221 (merged) |
| Owner-routed reports and root summaries | #223 (merged), pull request #329 |
| Root-only spawn and the 2/3/6 limits | #224 (merged), pull request #330 |
| Coordinator gate and per-project limits (ADR section 8) | #350, pull request #353: `ProjectConfig.Coordinators` in [`coordinators_config.go`](../../backend/internal/domain/coordinators_config.go) |
| Coordinator prompt and handoff | #225 (merged), pull request #331 |
| Ownership, recovery, and latest summary in session views | #226 (merged), pull request #335 |
| `MERGE_ROOT_ONLY` merge authority | pull request #333 |
| End-to-end pilot (CLI and service level) | #227, pull request #339: [`coordinator_pilot_e2e_test.go`](../../backend/internal/cli/coordinator_pilot_e2e_test.go), [`pilot_test.go`](../../backend/internal/service/coordinatorpilot/pilot_test.go) |
| Live multi-machine run and baseline comparison | **Pending operator run** on #227 and #218; capture protocol in [resource-baseline.md](resource-baseline.md) |
| Root escalation summary on coordinator termination | [#340](https://github.com/petarnenov/ultra-factory/issues/340) (closed): migration `0216` writes the `coordinator_terminated` summary |

**Merge authority.** The daemon refuses `ao pr merge` from every live worker and coordinator with `403 MERGE_ROOT_ONLY`, in every project and whether or not coordinators are enabled (introduced in 0.18.0, pull request #333). Integration belongs to the project root, so a worker reports a green PR to its owner and does not merge it.

### Prerequisites

| Prerequisite | Check |
| --- | --- |
| A hub build that contains the coordinator hierarchy and the gate | `ao --version`; the pilot, its dependencies, and the coordinator gate are on `dev` at or after commit `c292e21c`. |
| A healthy hub daemon and BFF | In a hub shell: `ao status`, then `/healthz` and `/readyz` on `http://127.0.0.1:3001`. |
| Exactly one live root for the pilot project | `ao orchestrator ls` lists one session, with the pilot project id. |
| No coordinator in the project yet | `ao coordinator ls -p <project>` prints `(no coordinators)`. |
| Coordinators enabled for the pilot project only | `ao project get <project> --json` shows `project.config.coordinators.enabled` true; every other project shows it false or absent. |
| Paired worker hosts online, none running a local orchestrator | `ao host ls` shows every host `online`; `ao orchestrator ls --host <H>` is empty for each host. A host with a local orchestrator refuses a worker from the hub with `LOCAL_ORCHESTRATOR_ACTIVE`. |
| The hub's memory limit and current headroom are recorded | Railway service **Metrics** tab and **Settings → Deploy → Replica Limits**; `railway usage limit status --target workspace`. Do not change them. |
| A resource baseline or a recorded gap | The six-worker capture in [resource-baseline.md](resource-baseline.md) is read-only and still **not run**. ADR section 9 says a missing baseline is recorded as a gap, not assumed. **Pending operator run.** |
| A fresh hub database and registry backup | [Hub database and registry backup](#hub-database-and-registry-backup), before the first pilot coordinator. **(guidance)** |
| Worker credentials and project dependencies on every host | The usual worker prerequisites ([Worker service](#worker-service)). Hub credentials are not forwarded. |

The pilot project, hosts, harnesses, and window are operator inputs; record them
in [Operator values to fill in](#operator-values-to-fill-in).

### Supported topology

```text
hub (coordination only)
  root orchestrator          kind=orchestrator, owner NULL, exactly one live
  coordinator A              kind=coordinator, scope A, Chat by default
  coordinator B              kind=coordinator, scope B, Chat by default
        │  remote links (owner = the coordinator's local session id)
        ▼
paired hosts (implementation only)
  up to six active workers total, local or remote; for example:
    shape A   3 owned by A + 3 owned by B (root owns none)
    shape B   2 owned by A + 3 owned by B + 1 root-owned
  a root-owned worker uses a slot in the same six, never an extra one
```

- **Coordinators run where the root runs.** The root spawns them on the hub's
  daemon; they are local sessions. `ao spawn --kind coordinator --host ...` is a
  usage error (exit 2), and a caller that is not the live root gets
  `403 COORDINATOR_ROOT_REQUIRED`.
- **Workers run on the paired hosts.** A coordinator can spawn a remote worker
  (`ao spawn --host` from its session) and owns it through the remote link. A
  root-owned direct worker keeps working exactly as before, needs no
  coordinator, and counts toward the same six.
- **Two shapes fit the cap.** Two coordinators with three workers each use all
  six slots; if the root keeps a worker directly, a coordinator holds fewer, so
  `2 + 3 + 1` also stays at six. The project total never exceeds six.
- **Ownership is a hub fact.** A remote link stores a local coordinator session
  id; that id is never sent to the peer daemon, so a worker host cannot see or
  address the topology.
- **One root per project.** A project has at most one live root; a second
  orchestrator spawn returns the live one, and a clean replacement retires only
  the root. Restoring a terminated root while another is live fails with
  `ORCHESTRATOR_ALREADY_ACTIVE`. Root replacement does not touch coordinators:
  they keep running and keep their workers.
- **No replication.** One daemon, one SQLite database, no shared volume, no
  second coordinator service.
- **Scopes are disjoint by operating rule, not by constraint.** Two live
  coordinators may share a scope; the root must not do that. **(guidance)**

### Capacity defaults

Default limits from ADR section 8, enforced in the daemon and read from the
project's `coordinators` config:

| Limit | Counted set | Default | Error |
| --- | --- | --- | --- |
| Live roots per project | `kind='orchestrator' AND is_terminated=0` | 1 | `ORCHESTRATOR_ALREADY_ACTIVE` |
| Live coordinators per project | `kind='coordinator' AND is_terminated=0` | 2 | `COORDINATOR_LIMIT_REACHED` |
| Active workers per coordinator | local `kind='worker'` with that owner **plus** `remote_worker_links` with that owner | 3 | `COORDINATOR_WORKER_LIMIT_REACHED` |
| Active workers per project | every live local worker **plus** every link in the project | 6 | `PROJECT_WORKER_LIMIT_REACHED` |

- A remote link counts as one active worker until it retires.
- Root-owned workers count toward the six, so two full coordinators leave no
  room for a seventh worker, direct or owned.
- Idle, waiting, hibernated, and crashed-not-yet-reaped sessions consume a slot;
  termination releases coordinator, owner, and project slots in the transaction
  that sets the terminal fact. A rejected spawn stores nothing and consumes no
  slot.
- Restoring a terminated worker re-checks both caps; restoring a terminated
  coordinator re-checks the two-coordinator cap and **does not** reattach its
  former workers.
- The defaults are 2/3/6; the hard maximum is 4/6/12. A project may set
  `maxCoordinators`, `maxWorkersPerCoordinator` and `maxWorkers` in its
  `coordinators` config block up to the hard maximum, and anything above it is
  refused with `COORDINATOR_LIMITS_INVALID`. The effective limits are read from the project
  config on every spawn, restore, reassignment, and remote-link write.

### Enable one pilot project

**The gate (ADR section 8).** One project config block gates and bounds the
feature, implemented as `domain.CoordinatorsConfig`:

```json
"coordinators": {
  "enabled": false,
  "maxCoordinators": 2,
  "maxWorkersPerCoordinator": 3,
  "maxWorkers": 6
}
```

- `enabled` defaults to `false`. A coordinator spawn while it is off is refused
  with `409 COORDINATORS_DISABLED` (`projectId` in the details), after the
  root-caller and scope checks and under the project lock.
- `maxCoordinators`, `maxWorkersPerCoordinator` and `maxWorkers` default to
  2/3/6. `0` means "keep the default". A value up to the hard maximum 4/6/12
  is allowed; a value above it or below zero is rejected with
  `COORDINATOR_LIMITS_INVALID` (400, `field`, `max`) when the config is set.
- Disabling blocks new coordinator spawns while restoring a terminated
  coordinator stays allowed for recovery, and existing ownership keeps routing
  so a rollback can reassign before the flag is turned off.
- The config is read and written through `ao project get <project> --json` and
  `ao project set-config <project> ...`, or
  `PUT /api/v1/projects/{id}/config`.

**Enable it for the pilot project only (guidance).** `set-config` replaces the
whole config, even when you pass one focused flag, so read the current object
first and keep every field you need:

```sh
ao project get <project> --json

# A project with no other config: the flag form is enough.
ao project set-config <project> --coordinators

# Optional: lower a limit for the pilot. 2/3/6 are the defaults; 4/6/12 is the maximum.
ao project set-config <project> --coordinators \
  --max-workers-per-coordinator 2
```

For a project that already has config, keep every field from
`ao project get` and add the block before submitting the complete object:

```json
"coordinators": {"enabled": true}
```

```sh
ao project set-config <project> --config-json '<full config JSON>'
```

- `{"enabled": true}` alone leaves the limits at 2/3/6; add `maxCoordinators`,
  `maxWorkersPerCoordinator`, or `maxWorkers` to lower them.
- Verify before spawning: `ao project get <project> --json` shows
  `project.config.coordinators.enabled` true. A spawn in another project still
  fails with `COORDINATORS_DISABLED`; keep it that way for the pilot.
- The ADR keeps the feature off until the pilot is approved: enabling one
  project is a pilot step, not approval for regular work.

Only the live project root can spawn a coordinator. Send the root the assignment
and let it spawn, or run the same call from a hub shell with the root's session
id:

```sh
# Preferred: from the root's own session, with the assignment brief from the
# operating contract.
ao spawn --project <project> --kind coordinator --scope "<one bounded area>" \
  --prompt "<assignment brief>"

# Operator start from a hub shell: the caller is checked by session id only,
# so AO_SESSION_ID selects the live root. The ADR treats this as an accident
# guard, not authentication.
AO_SESSION_ID=<root-session-id> ao spawn --project <project> \
  --kind coordinator --scope "<one bounded area>" --prompt "<assignment brief>"
```

- `--scope` is required (exit 2 when missing) and is at most 200 characters.
- The coordinator uses the project's orchestrator role config, like the root.
- Use the five-section brief from the
  [operating contract](../../docs/coordinators/operating-contract.md#assignment-brief-template).
  A missing section is not a valid assignment.
- To keep the pilot to one project, verify that no coordinator exists anywhere
  else: `ao session ls --all --include-terminated --json`, filtered to
  `"kind":"coordinator"`. **(guidance)**
- The daemon does not enforce "only this project"; the operator and the root
  do. Do not start coordinators from another project's root.

### Signals to monitor

Record every signal with a UTC timestamp. Use the hub's loopback listener from a
hub shell for hub-local reads; per-process worker memory is only readable on the
worker's own host.

| Signal | Source | Record |
| --- | --- | --- |
| Per-process memory | `ao session top --all -p <project> --json` and `GET /api/v1/usage/sessions/memory?projectId=<project>` | RSS and process count per root, coordinator, and worker; `null` means unmeasured, never zero |
| Hub memory headroom | `system` and `app` in the memory route; `GET /api/v1/usage/memory/pressure`; Railway Metrics tab and replica limit | Pressure verdict, AO total, service memory, and the limit in force |
| Coordinator liveness | `ao coordinator ls -p <project>`; `ao session ls` `HEALTH`; `ao session get <id> --json` | Terminated vs live-but-exited, `NEEDS RECOVERY`, latest summary |
| Capacity rejections | Caller transcript; daemon access log `status=409` on `POST /api/v1/sessions` and `PUT .../owner`; roster counts | Code, project/coordinator id, current and allowed counts |
| Pending report age | Coordinator summary history route; read-only report table query | Oldest pending age, claimed age, attempt count |
| Delivery errors | `lastError` on summaries; `last_error` on reports; access log 5xx lines | Message, attempt count, first and last seen |

#### Per-process memory and hub headroom

```sh
# In a hub shell.
ao session top --all -p <project> --json
curl -s 'http://127.0.0.1:3001/api/v1/usage/sessions/memory?projectId=<project>' \
  > /tmp/ao-coordinator-memory.json
curl -s 'http://127.0.0.1:3001/api/v1/usage/memory/pressure'
```

The memory route returns per-session `rssBytes`, `cpuPercent`, `processCount`,
and the per-process tree, plus host `system` headroom and `app` for everything
AO runs. `ao session top` gives RSS and process count only; it has no CPU, no
per-process list, and no `--host`, so run it on each host for that host's
workers.

**(guidance)** Use the same "near the limit" definition as the baseline
protocol: at or above 90% of the configured hub memory limit. The pilot's
comparison target is the six-worker baseline from #218; until that run exists,
the figure is **not measured** and must not be reported as one.

#### Coordinator liveness

```sh
# In a hub shell.
ao coordinator ls -p <project>
ao coordinator ls -p <project> --include-terminated
ao session get <coordinator-id> --json
ao session ls -p <project>
```

A coordinator that is terminated is not a live helper: its workers are returned
to the root and its slot is free. A coordinator that is live but whose agent has
exited keeps its slot and its ownership, but is not a live recipient: its
workers' reports go to the root, which gets one "needs recovery" notice per
exit. Restore it (`resume-agent`) to resume its reports, or terminate it.
Treat a long-lived `exited` coordinator as a rollback trigger. Session
health states and thresholds are in
[session health and stall runbook](../../docs/runbooks/session-health-stalls.md).

#### Capacity rejections

The requesting session sees the code; the CLI prints it and exits 1:

- `COORDINATOR_LIMIT_REACHED` (third live coordinator)
- `COORDINATOR_WORKER_LIMIT_REACHED` (fourth worker for one coordinator)
- `PROJECT_WORKER_LIMIT_REACHED` (seventh worker in the project)
- `COORDINATOR_ROOT_REQUIRED`, `OWNERSHIP_ROOT_ONLY` (wrong caller)
- `OWNER_REASSIGNMENT_IN_FLIGHT` (retry the ownership write)

The daemon access log records the HTTP status but not the code:
`http request ... path=/api/v1/sessions status=409`. Pair it with the caller's
transcript, and read the current counts rather than inferring them:

```sh
# In a hub shell.
ao coordinator ls -p <project>        # local owned workers and root-owned count
ao remote-worker ls -p <project>      # remote links and their owners
```

The project total is every live local worker plus every link in the project,
including root-owned ones.

#### Pending report age and delivery errors

The coordinator summary history is the API-visible signal; each entry carries
`deliveryState` (`pending`, `claimed`, `acknowledged`), `deliveryAttempts`,
`lastError`, and `createdAt`:

```sh
curl -s 'http://127.0.0.1:3001/api/v1/sessions/<coordinator-id>/summaries?limit=20'
```

The reports projection (`GET /api/v1/reports?projectId=<project>`) deliberately
does not expose delivery state. For worker reports, read the daemon's database
read-only; the hub image ships `sqlite3`:

```sh
sqlite3 -readonly /data/ao/ao.db "
  SELECT delivery_state, COUNT(*) AS reports,
         MIN(created_at) AS oldest, MAX(delivery_attempts) AS max_attempts
  FROM reports WHERE project_id = '<project>'
  GROUP BY delivery_state;"

sqlite3 -readonly /data/ao/ao.db "
  SELECT id, session_id, delivery_state, delivery_attempts, created_at, last_error
  FROM reports
  WHERE project_id = '<project>' AND last_error <> ''
  ORDER BY created_at DESC LIMIT 20;"
```

**(guidance)** This is a read-only schema inspection, not an API contract; never
write to the database. A worker host can still hold reports the hub has not
pulled yet, so run the same query on that host's database when a link is down.
The ADR accepts that pending age is an operator signal: a root outage leaves
root-addressed work pending by design, and helper delivery continues.

### Stop new coordinator spawns

Set `coordinators.enabled` to `false` for the project. That is the whole
switch: new coordinator spawns fail with `409 COORDINATORS_DISABLED`, restore of
a terminated coordinator stays allowed, and every existing owner keeps routing.
`set-config` replaces the whole config, so read the current object first and
submit the complete one:

```sh
ao project get <project> --json
ao project set-config <project> --config-json '<full config JSON>'
```

- Disabling stops new spawns; it does not stop a running coordinator or
  reassign its workers. Continue with the drain and reassignment below.
- Record the time of the change and the roster snapshot
  (`ao coordinator ls -p <project>`).
- Do not terminate the root as a way to "disable" the feature: that stops
  helper summaries from reaching a root and leaves root-addressed work pending.

### Drain or reassign a coordinator's workers

There is no coordinator drain command. Draining is an ordered sequence, and the
BFF's Railway worker drain is unrelated: it gates work to a Railway worker
service and never touches coordinator ownership. **(guidance)**

1. Ask the coordinator for a final summary: `done` when its scope is complete or
   `blocked` with the reason when it is not. Its system prompt and the operating
   contract require it to stop acting on a worker the root reassigns.
2. Let active workers reach a checkpoint (pull request open, checks running) or
   finish. Do not kill worker sessions to drain a coordinator.
3. Reassign every worker to the root, or to another live coordinator with a free
   slot, from the root's session:

   ```sh
   ao session assign <worker-id> --owner root
   ao session assign <worker-id> --owner <other-coordinator-id>

   # Linked remote workers use the link identity:
   ao remote-worker assign <host-id> <remote-session-id> --owner root
   ao remote-worker assign <host-id> <remote-session-id> --owner <other-coordinator-id>
   ```

4. Retry `409 OWNER_REASSIGNMENT_IN_FLIGHT`: a report of that worker is claimed
   for the old owner, and the write succeeds once the claim is released or
   acknowledged. Do not delete the report to clear the error.
5. Verify before moving on (next section). Terminate the coordinator only after
   its workers have an owner you intend.

What this does **not** do:

- It does not delete a worker session, worktree, report, or summary.
- It does not move a report that is already claimed; it routes on the next
  claim.
- It does not change the project total when the target is the root.
- It does not reattach workers when a terminated coordinator is restored; the
  root assigns them explicitly.

### Return the project to root-owned workers

Run every write from the live root (the CLI uses `AO_SESSION_ID`; pass
`--requested-by <root-id>` when the command does not run in the root's session,
and a `--host` target always needs it). Verify ownership at the root **before**
terminating anything:

```sh
# Each worker: owner = root, project root resolved.
ao session owner <worker-id>

# The whole hierarchy, including terminated coordinators.
ao coordinator ls -p <project> --include-terminated

# Remote links and their local owners.
ao remote-worker ls -p <project>

# Listing view: the OWNER column is "root" for root-owned workers.
ao session ls -p <project>
```

The expected result after reassignment:

- every worker and remote link has owner `NULL` (printed as `root`), and
  `ao session owner` names the live root as the project root;
- each coordinator shows `workers: 0` (or is still listed with the workers it
  owns until you reassign them);
- `needsRecovery` is false for every coordinator;
- the coordinator sessions still exist, with their scopes and summaries.

Only then terminate the coordinator:

```sh
ao session kill <coordinator-id>
```

Termination returns any worker or link it still owns to the root atomically,
audits the change and writes the `coordinator_terminated` escalation summary to
the root (migration `0216`, [#340](https://github.com/petarnenov/ultra-factory/issues/340)).
It does **not** delete the coordinator, its workers, or their reports. Still
confirm the handback with the commands above.

Do not run `ao session cleanup` during this sequence: it reclaims workspaces of
terminated sessions (skipping dirty worktrees). **(guidance)** Keep worktrees
until ownership and delivery are confirmed.

### Check before declaring a rollout complete

The pilot is complete only when every row is recorded with evidence, on the
issue, not only in chat. **(guidance)** for the checklist itself; each item is a
consequence of ADR 0010 or the merged implementation.

- [ ] One live root; at most two live coordinators; scopes are disjoint and
      documented in the roster.
- [ ] Six active workers at most, three per coordinator; every owner is correct
      in `ao session owner` and in the remote-link list.
- [ ] Owner-routed delivery is observed: each worker's report reaches its owner
      once, and a duplicate is not delivered twice.
- [ ] Summaries and escalations reach the root; `deliveryState` becomes
      `acknowledged` and `lastError` stays empty.
- [ ] Capacity refusals are observed with the expected codes, return exit 1, and
      leave no session row or consumed slot.
- [ ] Coordinator termination returns workers and links to the root; a
      replacement coordinator can take them; restore does not reattach them.
- [ ] Only the root merges: a coordinator or worker gets `403 MERGE_ROOT_ONLY`;
      the root passes.
- [ ] A hub daemon restart with a delivery in flight loses nothing and does not
      duplicate a claim or an acknowledgement. **(Pending operator run:** the
      service-level pilot covers restart and requeue; the live process restart
      is part of #227.)
- [ ] Per-process memory for the root, both coordinators, and every worker is
      recorded, and the hub service memory stays within the agreed headroom
      against the #218 baseline. **(Pending operator run)**
- [ ] Disabling and rollback were rehearsed in an isolated environment
      (`AO_DATA_DIR` scratch data, never production), and the ownership checks
      above were run there.
- [ ] The gate is enabled only for the pilot project: `ao project get <project>
      --json` shows `project.config.coordinators.enabled` true, every other
      project shows it false or absent, and a coordinator spawn in a disabled
      project returns `409 COORDINATORS_DISABLED`.

### Rollback steps

Run in this order. The goal is root-owned workers with every pending report and
summary delivered, and **no** coordinator, worker, report, or summary deleted.

1. **Record the trigger and evidence.** UTC time, deployed revision, project id,
   roster before (`ao coordinator ls -p <project>`), remote links, pending
   counts, and the signal that triggered the rollback.
2. **Stop new coordinator spawns.** Set `coordinators.enabled=false` for the
   project: read `ao project get <project> --json`, keep every field, and submit
   the complete object with `--config-json`. New spawns fail with
   `409 COORDINATORS_DISABLED`; restore and routing stay allowed. Do not
   terminate the root.
3. **Drain.** Ask each coordinator for a final `done` or `blocked` summary; let
   workers reach a checkpoint; start no new tasks.
4. **Reassign every worker and remote link to the root.** `ao session assign
   <worker> --owner root`; `ao remote-worker assign <host> <remote-session>
   --owner root`. Retry `OWNER_REASSIGNMENT_IN_FLIGHT`; never delete a report or
   a link to clear it.
5. **Verify ownership at the root before terminating.** `ao session owner` for
   every worker, `ao coordinator ls --include-terminated`,
   `ao remote-worker ls`. Every worker must resolve to the live root.
6. **Terminate each coordinator.** `ao session kill <coordinator-id>`. Keep the
   rows; termination is not deletion. If you skipped step 4, the termination
   trigger still returns the workers to the root and writes the escalation summary;
   verify both manually.
7. **Verify pending reports and summaries reach the root.** Check the summary
   history (`deliveryState`, `lastError`) and the report-table query above; the
   root's conversation must show the handback. A live-but-exited coordinator
   no longer blocks its workers' reports (they go to the root), but restore or
   terminate it in step 6 first.
8. **Verify nothing was deleted.** Coordinators still appear with
   `--include-terminated`; worker sessions, worktrees, reports, and summaries
   are intact; no `needsRecovery`; `ao session cleanup` has not run.
9. **Keep the feature off.** Leave `enabled=false` for every project. The schema
   stays; coordinator rows stay terminated.
   Do not downgrade the binary while any `kind='coordinator'` row exists: an
   older daemon does not understand the value, and the schema Down migration is
   not a rollback path.
10. **Record the outcome** on #227/#228: what was reassigned, what delivered,
    what is still pending, and the exact revision. Do not delete the pilot
    project's records to "clean up".

If a step fails, stop and keep the feature off. Do not force-delete a worktree,
delete a link, or drop the database; inspect with the roster, the ownership
commands, and the read-only report query, and escalate with the evidence.

### Criteria to disable the pilot

**(guidance)** These are operator thresholds, not ADR rules. The ADR only says
limits may be lowered without a schema change and that disabling blocks new
spawns while keeping routing and restore. Disable and roll back when any of the
following holds for longer than the agreed observation window:

- Hub service memory is repeatedly at or above 90% of its configured limit, or
  the measured coordinator cost makes the comparison against #218 fail.
- A coordinator is live but its agent is exited with pending summaries or
  reports that are not draining.
- A capacity rejection blocks planned work while the census shows a free slot,
  or a rejected spawn consumed a slot.
- Pending report or summary age exceeds the agreed threshold, `lastError`
  repeats, or a report is acknowledged without an accepted turn.
- `needsRecovery` is true for any coordinator, or a handback leaves a worker
  pointing at a terminated owner.
- A duplicate delivery, a lost report, or a merge by a non-root session is
  observed.

### Operator values to fill in

| Value | Where it goes |
| --- | --- |
| Pilot project id and display name | This runbook's copy of the record; issue #227 |
| Hub service, environment, memory limit in force | Pilot record; compare with #218 |
| Paired host labels and harness assignment (six workers) | Pilot record |
| Observation window (UTC start/end), sample interval, repeat window | Pilot record; use [resource-baseline.md](resource-baseline.md) |
| #218 baseline peak RSS and hub headroom, or "not measured" | Pilot record; the comparison section of the baseline template |
| Agreed pending-report age threshold | Pilot record, before the run |
| Approval decision (who, when) and rollback owner | Pilot record |
| Deployed revision/version and the run's head commit | Pilot record; issue #227 |

Until these values are filled from a real run, the runbook is complete but the
rollout is **not approved**.

## Local end-to-end test

[`compose.e2e.sh`](compose.e2e.sh) (RW-4.3.1) runs the hub and one worker
locally with Docker Compose and drives the web UI with Playwright. It needs
Docker and Node 24, and it contacts no Railway service.

```bash
./deploy/railway/compose.e2e.sh
```

The script generates a test-only operator password hash, creates a bare Git
fixture repository, builds the worker image without agent CLIs plus a fake
`claude` (`Dockerfile.worker-e2e`), starts
[`compose.e2e.yaml`](compose.e2e.yaml), and runs
`frontend/e2e/railway/hub.spec.ts` with `frontend/playwright.railway.config.ts`.
The hub uses `AO_HUB_PROVIDER=fake`, which "creates" a worker by returning the
address of the Compose `worker-1`; identity probing, bearers, the proxy, and
the `/mux` terminal stay real. The test signs in, adds a worker by address,
provisions one through the fake provider, opens the host route, clones the
fixture, starts a fake-agent session, checks live terminal output, and removes
the worker. The hub is published only on `127.0.0.1:8080`. The script tears
everything down, volumes included, on exit and prints the worker log on
failure.

CI runs the same script in
[`railway-e2e.yml`](../../.github/workflows/railway-e2e.yml) on pull requests
that touch `deploy/railway/`, `packages/hub-bff/`, `frontend/src/renderer/`, or
the Railway e2e files. `AO_HUB_PROVIDER=fake` refuses to start together with
Railway project or environment IDs; never set it on a Railway service.

The workflow needs a host runner. The script drives the UI at
`http://localhost:8080` and the hub publishes only on the daemon's loopback,
so the Playwright client has to share the Docker host: a GitHub-hosted runner
and a local machine both work, but a containerized runner that reuses the host
daemon over the Docker socket (DooD) does not, because `localhost` there is the
runner container rather than the host. Run `compose.e2e.sh` directly to
validate locally.

## Logs and troubleshooting

Use the selected Railway service's deployment logs. On the hub, the supervisor
writes startup and process-exit messages to stderr; the BFF writes structured
JSON request logs to stdout and excludes passwords, hashes, cookies, and
headers. With human access, `railway ssh` into the explicitly selected service
and run `ao status` and `ao remote-host status` there. The latter prints the
password: do not attach raw output to an issue. Inspect `/healthz` and
`/readyz` at `http://127.0.0.1:3001` privately. No Railway CLI version or
service selection is assumed by these instructions.

| Symptom | Repository-backed diagnostic |
| --- | --- |
| Entrypoint or supervisor exits before the daemon starts | Check `/data` mount and ownership; the entrypoint requires writable `HOME` and `AO_DATA_DIR`, and points to `RAILWAY_RUN_UID=0`. |
| Hub crash loop after a deploy | The `hub-image.yml` 60 s re-check fails the job. Read the supervisor and BFF lines in the deployment log; roll back with [Hub rollback](#hub-rollback). |
| Login returns `429` or `503` | `429`: 5 attempts per minute per client; wait for `Retry-After`. `503 busy`: 2 password checks already running; retry. |
| Login returns `403` | Missing or mismatched `Origin` (or `AO_WEB_ORIGIN` set to another origin). |
| Loopback health passes but a host is offline | Check the public domain target against the actual listener port and identity, or the private address. Loopback health alone does not test Railway routing. |
| `unauthorized`, or `502 host credential rejected` in the web UI | Update the stored password (desktop connection or `hosts.json`); changing the first-start environment variable alone does not change the stored password. |
| `identity-changed` or `needs-pairing` | Confirm the correct volume/installation and re-pair intentionally. Do not bypass the identity guard. |
| Temporary lockout | Five failed attempts trigger a one-minute source lockout. Verify proxy configuration against ADR 0005; never disable Bearer authentication. |
| Worker stuck in provisioning or `failed` | Read the sanitized `lastError` in Settings → Workers and the worker's deployment log. Provisioning gives up after 10 minutes without an identity answer. |
| Clone, agent or PR fails | Check credentials and project dependencies on that host; laptop and hub credentials are not forwarded. Avoid printing environment values. |
| Preview returns a managed-server notice | The desktop remote proxy emits this for an app preview requiring a managed server; start the existing configuration in that worker session. |
| Remote control route returns 404 | Expected for the listener's blocked routes, including mobile controls, browser/desktop controls, side-chat launch, system install, Codex accounts and session `/preview/server`. Use operator loopback access where appropriate; do not publish port `3001`. |

Repository sources for the worker instructions: the linked worker files,
`backend/internal/cli/remote_host.go`, `backend/internal/cli/remote_target.go`,
`backend/internal/httpd/lan_listener.go`, `backend/internal/httpd/controllers/mobile.go`,
`frontend/src/main/remote-proxy.ts`, and the desktop's
`frontend/src/renderer/components/settings/RemoteHostsSettings.tsx` and
`frontend/src/renderer/components/CreateProjectFlow.tsx`.

## Validation record

For each live check, record the date, deployed revision or digest, client
version, host/project/session IDs (masked), expected and actual outcome,
pass/fail or partial status, and sanitized evidence (including any test PR
URL). Carry each failed or partial result into tracking before acceptance.

### Hub validation record (RW-2.5.3)

First live deployment on 2026-10-17 ([issue #104](https://github.com/petarnenov/ultra-factory/issues/104)),
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
| Orchestrator credentials (RW-5.4.1) | Pass (2026-10-17, [#134](https://github.com/petarnenov/ultra-factory/issues/134)) | In the hub, `ao agent ls` listed `claude-code` and `codex` as `authorized`; the hub orchestrator session answered prompts and ran `gh issue view 119 --repo petarnenov/ultra-factory`. With `AO_HUB_ORCHESTRATOR_TICK=1` the hub tick loop delivered TICKs to it (for example for PR #155) with nothing running on the Mac. No credential output recorded. |
| Orchestrator wake-up loop (RW-5.4.2) | Pass (2026-10-17, [#134](https://github.com/petarnenov/ultra-factory/issues/134)) | With `AO_HUB_ORCHESTRATOR_TICK=1` on the hub, the tick loop delivered TICKs to the hub orchestrator (for example for PR #155) with nothing running on the Mac. Recorded from the operator's live check; no dry-run output. |

The spike in [`packages/hub-bff/docs/railway-api-notes.md`](../../packages/hub-bff/docs/railway-api-notes.md)
confirmed only selected Railway GraphQL create/configuration operations with a
public test image. Hub startup and hub → worker private networking were
verified later in the deployment above. Image timing, volume backup, and
deletion permissions remain open and must not be reported as passed.

### Worker verification log (RW-1.4.1)

Read-only checks on 2026-10-17 against project `<project-name>`, environment
`<environment-name>`, service `<service-name>`, supporting RW-1.4.2
([issue #17](https://github.com/petarnenov/ultra-factory/issues/17)). No
Railway resources were created, deleted, restarted, redeployed, or
reconfigured. The Railway CLI credential remained local. The service's
configured bearer credential was supplied only to authenticated HTTPS probes to
this service. Neither value was printed or recorded.

| Check | Status | Sanitized evidence |
| --- | --- | --- |
| Railway target | Pass | Project `<project-name>` (`<project-id>`), environment `<environment-name>` (`<environment-id>`), service `<service-name>` (`<service-id>`). |
| Active deployment | Pass | Deployment `<deployment-id>` is `SUCCESS`/`RUNNING`, created `2026-10-17`, commit `<commit>` on `feat/railway-hosting`, image digest `<image-digest>`. |
| Source/build | Pass | Repository `petarnenov/ultra-factory`, Dockerfile `deploy/railway/worker/Dockerfile`, root build context (`rootDirectory=null`). |
| Volume | Pass, persistence configured | `<volume-name>` is `READY`, 50 GB, mounted at `/data`. Read-only metadata showed `/data/ao/ao.db` present (2,154,496 bytes); database contents were not read. |
| Public HTTPS and identity | Pass | `https://<worker-domain>/api/v1/identity` returned HTTP 200 with TLS verification result 0, API version 1, and host ID `<host-id>`. |
| Readiness and authenticated route | Pass | On HTTPS, `/readyz` and `/api/v1/sessions` returned 401 without authentication and 200 with the local bearer credential. No credential value was output. |
| Public routing | Pass | Domain `<worker-domain>` is `ACTIVE` and targets port `3011`. |
| Main listener port `3001` | Pass, loopback-only | Read-only `/proc/net/tcp` and `/proc/net/tcp6` inspection showed `127.0.0.1:3001` and no wildcard listener on that port at inspection time. |
| Remote listener port `3011` | Pass, dual-stack | `/proc/net/tcp6` showed a wildcard listener on 3011 and `bindv6only=0`; the configured `AO_REMOTE_BIND` is `dual` and `AO_REMOTE_TRUSTED_PROXY` is `1`. |
| Private-network reachability | Pass, self-route | From the service container, `<service>.railway.internal` resolved to IPv4 and IPv6. Requests to `http://<service>.railway.internal:3011/api/v1/identity` returned 200 over both families. Hub → worker reachability was verified later (see the hub record). |
| Host identity across observed deployments | Pass, limited | Current host ID matches the prior observation before the latest deployment. This does not independently establish database continuity. |
| Database preservation across redeploy | Unverified | No redeploy or pre/post database marker/checksum comparison was performed. A deliberate redeploy and sanitized state comparison are needed. |
| Five-failure lockout and client isolation | Unverified | No wrong-password sequence was sent, to avoid locking out a client. Requires an isolated test source and a second source IP or equivalent sanitized logs. |
| Readiness timing, image size, idle memory | Unverified | No timed deployment or idle measurement was performed; image size was not available from these read-only checks. |

An earlier independent client probe on 2026-10-16 observed public identity HTTP
200 and timed out when probing public port 3001. The in-container socket
inspection independently confirms that the main listener binds only to
loopback.

### Pending live checks

Every item below is unverified. Do not report it as passed until its result is
recorded above.

| Check | Current evidence | Tracking |
| --- | --- | --- |
| Board load time, SSE delivery latency, terminal typing latency | Login asset TTFB only; streams open; terminal output works | [#141](https://github.com/petarnenov/ultra-factory/issues/141) |
| PR status in the browser, full sweep of desktop-only features on the web, image CSP (`img-src`) decision, `make check` green on the branch head | #127 hid Codex accounts and enabled remote hosts; no full sweep | [#142](https://github.com/petarnenov/ultra-factory/issues/142) |
| Hub backup export and restore, worker drain, worker redeploy with its volume, resume after restart, hub rollback, `volume_size_limit`, idle warning | Local tests only | [#145](https://github.com/petarnenov/ultra-factory/issues/145) |
| Railway token deletion permissions (service and volume) for worker removal | RW-3.1.1 token was denied deletion | Operator decision before setting `RAILWAY_API_TOKEN` |
| Worker provisioning through the Railway API on a live hub, including `AO_HUB_WORKER_URL_MODE=private` | Fake provider in the local e2e only | Needs the token above |
| Desktop pairing and host-scoped clone/session/terminal/message/PR/preview on a Railway worker (RW-1.4.2) | Not run from the desktop app; the web UI path to `worker-1` works | [#17](https://github.com/petarnenov/ultra-factory/issues/17) |
| CLI spawn/list/get/send against a Railway worker | Not tested | [#17](https://github.com/petarnenov/ultra-factory/issues/17) |
| Worker database preservation across a deliberate redeploy | Host ID matched across deployments; no DB comparison | #145 |
| Five-failure lockout and client isolation through Railway's proxy | Not run | Operator, isolated test source |
| Worker and hub password/account-token rotation | Behavior inspected in source; not run | Operator maintenance window |
| Readiness timing, image size, idle memory | Not measured | Operator |
| Per-process resource baseline (#218) | Protocol drafted, not run; the 23.84 GB figure is unverified | Operator, [resource-baseline.md](resource-baseline.md) |
| Custom domain DNS/TLS for the hub | Only the Railway-generated domain was used | Operator, if a custom domain is added |
