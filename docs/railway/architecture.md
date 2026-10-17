# AO on Railway: architecture

Status: current for the `feat/railway-hosting` branch after RW-4.3.1. This page
describes how the Railway deployment is put together and how requests flow
through it. How to deploy and operate it is in the runbook,
[`deploy/railway/README.md`](../../deploy/railway/README.md). Security controls
and residual risk are in the [threat model](threat-model.md). The general,
non-Railway way to run AO on several machines is
[self-hosted remote hosts](../self-hosted-remote.md); the Railway deployment is
built on the same remote-host listener and host registry model.

Decisions behind this design:

| ADR | Decision |
| --- | --- |
| [0005](../adr/0005-trusted-proxy-remote-host.md) | The remote-host listener may key its lockout on the rightmost `X-Forwarded-For` entry, only with `AO_REMOTE_TRUSTED_PROXY=1`, never for authorization. |
| [0005](../adr/0005-daemon-pulls-remote-worker-reports.md) | An orchestrator's daemon pulls `ao report` deliveries from linked remote workers. All traffic goes hub → host. |
| [0006](../adr/0006-web-bff-for-hub.md) | A separate web BFF in front of the hub daemon. It talks only to authenticated listeners with a bearer and never proxies the loopback `127.0.0.1:3001` listener. |
| [0007](../adr/0007-dual-stack-remote-host-listener.md) | The `0.0.0.0` remote-host bind is already dual-stack in Go and must not be narrowed to `tcp4`. |
| [0008](../adr/0008-hub-egress-to-off-railway-hosts.md) | Accepted: one loopback-only SOCKS5 listener in the hub (Tailscale userspace) for tailnet destinations only. Not implemented on this branch; tracked by RW-5.2. |

## Components

```
Browser ──HTTPS──▶ Railway edge (TLS, HSTS)
                    │  $PORT (default 8080)
                    ▼
┌─ Railway service "hub" (deploy/railway/hub) ─────────────────────────────┐
│ hub-supervisor.sh (root: prepares /data, then runs children as uid 10001) │
│                                                                          │
│  hub-bff (Node 24, packages/hub-bff) on [::]:$PORT                       │
│   ├─ /login, session cookie, CSRF, login throttle                        │
│   ├─ static SPA (frontend build:web → /opt/ao/hub/dist-web)              │
│   ├─ proxy: /api/*, /readyz, /mux        → hub daemon 127.0.0.1:3011     │
│   ├─ proxy: /hosts/:hostId/*             → worker listener :3011         │
│   ├─ /hub/hosts, /hub/workers, /hub/sessions, /hub/audit                 │
│   └─ Railway GraphQL client (only when RAILWAY_API_TOKEN is set)         │
│                                                                          │
│  ao daemon                                                               │
│   ├─ 127.0.0.1:3001  primary listener, unauthenticated, never proxied    │
│   └─ 127.0.0.1:3011  remote-host listener, bearer, loopback only         │
│                                                                          │
│  ao-orchestrator-tick.sh loop (optional, AO_HUB_ORCHESTRATOR_TICK=1)     │
│                                                                          │
│  volume /data: ao/ (DB, worktrees, mobile/config.json), hub/hosts.json,  │
│                hub/audit.jsonl, home/ (agent logins, npm prefix)         │
└──────────────────────────────────────────────────────────────────────────┘
        │ bearer + X-AO-Expected-Host-ID
        │ private: http://<service>.railway.internal:3011
        │ or public: https://<worker-domain> (Railway TLS → 3011)
        ▼
┌─ Railway service "ao-worker-<id>" or a manually created worker ──────────┐
│ ao-worker-entrypoint.sh (root: prepares /data, then uid 10001)            │
│  ao daemon                                                               │
│   ├─ 127.0.0.1:3001  primary listener, unauthenticated, not published    │
│   └─ [::]:3011       remote-host listener, bearer, dual-stack            │
│  agent CLIs (Claude Code, Codex, ...), git, gh, tmux                     │
│  volume /data: ao/ (DB, worktrees, mobile/config.json), home/            │
└──────────────────────────────────────────────────────────────────────────┘

hub-bff ──HTTPS (Project-Access-Token)──▶ Railway public GraphQL API
GitHub Actions hub-image.yml ──▶ GHCR ao-hub image ──▶ Railway "hub" redeploy
```

- **Hub.** One Railway service built from
  [`deploy/railway/hub/Dockerfile`](../../deploy/railway/hub/Dockerfile). It
  extends the worker image, so the hub daemon can also run agent sessions
  (for example a hub orchestrator). The supervisor starts the daemon, the BFF,
  and the optional orchestrator wake-up loop, and seeds the hub daemon's
  remote-host listener as loopback-only on port `3011`.
- **BFF.** [`packages/hub-bff`](../../packages/hub-bff/README.md), a
  dependency-free `node:http` server. It is the hub's only public surface. It
  owns the single-operator login, serves the web build of the renderer, keeps
  the host registry (`hosts.json`), and proxies to daemons with the bearer it
  stores for each host. The browser never sees a bearer.
- **Workers.** Railway services built from
  [`deploy/railway/worker/Dockerfile`](../../deploy/railway/worker/Dockerfile)
  ([worker image README](../../deploy/railway/worker/README.md)). Each one is an
  ordinary AO daemon with the remote-host listener enabled, its own volume, and
  its own agent and GitHub credentials. A worker is either created by the BFF
  through the Railway API (`ao-worker-<id>`) or created by the operator and added
  by address.
- **Host registry.** `/data/hub/hosts.json` holds every host the BFF can reach:
  the automatic `hub` entry (built from the hub daemon's `mobile/config.json`),
  manual hosts, and Railway workers. Every host is probed at startup and every
  30 s.
- **Web renderer.** The same React renderer as the desktop app, built with
  `npm --prefix frontend run build:web` (`VITE_AO_WEB=1`). It uses relative,
  same-origin URLs, so all traffic goes through the BFF.

## Trust boundaries

- The public internet reaches only the BFF on `$PORT`. Neither daemon port is
  published on the hub.
- Inside the hub container, the BFF reaches the hub daemon only through the
  bearer-authenticated listener on `127.0.0.1:3011`. It refuses any host entry
  that targets port `3001`.
- A worker's `3011` is reachable from the hub over Railway's private network
  (plain HTTP inside Railway's network) or over its public Railway domain
  (HTTPS). The bearer is required on every request; the lockout uses the
  rightmost `X-Forwarded-For` entry only when `AO_REMOTE_TRUSTED_PROXY=1`
  (ADR 0005).
- Access to the web UI is a shell on every host the hub reaches. See the
  [threat model](threat-model.md) for assets, attackers, and residual risk.

## Flows

### Login

1. An unauthenticated page navigation gets `302 /login`; any other request gets
   a bare `401 {"error":"unauthorized"}` with no host detail. Only `/healthz`,
   `/login`, and the login page's own assets are public.
2. The login page is served by the BFF, not by the SPA, because the SPA shell
   itself is behind the session (ADR 0006).
3. `POST /login` with a JSON password, a matching `Origin`, and no cross-site
   `Sec-Fetch-Site`. The BFF verifies the password against the scrypt
   `AO_WEB_PASSWORD_HASH`. It allows 5 attempts per minute per client (the
   rightmost valid `X-Forwarded-For` entry) and at most 2 concurrent checks
   (`503` beyond that).
4. Success returns `204` and the `ao_hub_session` cookie: a random 256-bit id,
   `HttpOnly; Secure; SameSite=Strict; Path=/`. Sessions live in BFF memory with
   a 7-day idle and 30-day absolute lifetime. A redeploy signs everyone out.
5. `POST /logout` ends the current session; `DELETE /hub/sessions` ends every
   session. Login success and failure go to the audit log.

### REST

The renderer calls same-origin paths. The BFF picks the upstream by prefix:

- `/api/*` (including preview and artifact file routes) and `/readyz` go to the
  hub daemon at `http://127.0.0.1:3011`.
- `/hosts/:hostId/*` selects a registered, paired host. The prefix is removed
  before forwarding. A host that is not registered or not paired answers `404`.

For every proxied request the BFF rejects unsafe paths (`..`, `//`, encoded
separators, NUL), strips the browser's `Authorization`, `Cookie`, `Origin`,
client-IP forwarding, and hop-by-hop headers, and injects the host's bearer and
`X-AO-Expected-Host-ID`. It never forwards upstream `Set-Cookie`. An upstream
`401` becomes `502 host credential rejected`, and connection failures become
`502` with a generated `requestId`; neither returns the upstream body or URL nor
logs the browser out.

### SSE

Event streams use the same prefixes as REST. The BFF streams the response with
no idle timeout and closes the upstream request when the browser disconnects.
While the board is open there are long-lived BFF → hub and BFF → worker
streams. Delivery latency has not been measured yet ([#141](https://github.com/petarnenov/ultra-factory/issues/141)).

### WebSocket

Terminals use the `/mux` WebSocket, at `/mux` for the hub and
`/hosts/:hostId/mux` for a worker. The upgrade needs a valid session and the
same `Origin` check as a mutation. The BFF pipes the upgrade with the injected
bearer. A transport error after the upgrade closes the browser socket with code
`1011` and no detail. Live terminal output on a worker session was verified on
2026-10-17 ([#104](https://github.com/petarnenov/ultra-factory/issues/104)); typing latency is pending ([#141](https://github.com/petarnenov/ultra-factory/issues/141)).

### Railway API (worker management)

Worker management is active only when `RAILWAY_API_TOKEN`,
`RAILWAY_PROJECT_ID`, `RAILWAY_ENVIRONMENT_ID`, and `AO_HUB_WORKER_IMAGE` are
set. The runbook currently recommends leaving the token unset until deletion
permissions are verified.

1. **Startup.** The BFF calls Railway's `projectToken` query with the
   `Project-Access-Token` header and refuses to start unless the token's project
   and environment match the configured IDs. Account-wide tokens are not
   accepted.
2. **Create.** `POST /hub/workers` checks the worker limit
   (`AO_HUB_MAX_WORKERS`, default 5) and the request, records the worker in
   `hosts.json` with a generated listener password, writes an audit entry, and
   returns `202`. In the background the BFF creates a service named
   `ao-worker-<id>` from the worker image (`serviceCreate`), a volume at `/data`
   (`volumeCreate`), and a public domain when `AO_HUB_WORKER_URL_MODE=public`
   (`serviceDomainCreate`), then deploys it. It polls the deployment and the
   worker's identity route for up to 10 minutes, then pairs the host. The status
   goes from provisioning to online, or to `failed` with a sanitized error.
3. **Reconcile.** At startup the BFF resumes recorded provisioning and removing
   jobs. Every 5 minutes it lists the project's services and reports
   `ao-worker-` services without a registry record as orphans. It never adopts
   or deletes them.
4. **Remove.** `DELETE /hub/workers/:id` deletes the service and its volume
   (`serviceDelete`, `volumeDelete`). A failed deletion keeps the registry record
   for manual cleanup. Removing a manual host (`DELETE /hub/hosts/:id`) touches
   nothing in Railway.
5. **Drain.** `POST /hub/workers/:id/drain` marks a Railway worker as draining so
   the BFF refuses new work for it. It never calls Railway.

The BFF's Railway client is separate from CI: on pushes to
`feat/railway-hosting`, [`hub-image.yml`](../../.github/workflows/hub-image.yml)
publishes the hub image and redeploys the `hub` service through Railway's
`serviceInstanceUpdate` and `serviceInstanceDeployV2` operations. Every deploy
restarts the hub.

The local e2e (`deploy/railway/compose.e2e.sh`, RW-4.3.1) replaces the Railway
client with a fake provider (`AO_HUB_PROVIDER=fake`) that "creates" a worker by
returning the address of a Compose worker. Identity probing, bearers, the proxy,
and the terminal stay real.

### Worker reports and orchestration

A hub orchestrator delegates to workers with `ao spawn --host`. The hub daemon
then pulls each linked worker's pending `ao report` deliveries (ADR 0005,
daemon pulls), probing identity before sending the bearer. Traffic is always
hub → worker; workers never call the hub. PR and CI changes are not reports, so
the optional wake-up loop (`AO_HUB_ORCHESTRATOR_TICK=1`) nudges the orchestrator
when they change.

## What does not work on the web

The web build turns off every capability that needs the Electron supervisor
(`resolveCapabilities` in `frontend/src/renderer/lib/platform.ts`):

- the embedded browser panel, the agent CDP browser, and browser-profile import
  (Settings → Browser profiles is hidden);
- editor handoff ("open in editor") and the CLI link;
- auto-update of the app, the tray, native dialogs, and native window chrome.

Other differences:

- **Settings.** Codex accounts (Settings → Agents) are hidden. Remote hosts are
  always shown (no Developer mode or AO Cloud sign-in needed) and are backed by
  the hub's registry, without the desktop's edit action. Settings → Workers
  exists only on the web.
- **Notifications and shortcuts.** Desktop notifications become Web
  Notifications (a Settings toggle). Application shortcuts are handled by a
  keydown listener inside the page instead of the Electron main process, so a
  chord the browser handles itself may never reach AO.
- **Routes the remote-host listener blocks.** The BFF reaches every daemon,
  including the hub's own, through `3011`. That listener does not serve mobile
  controls, browser and desktop controls, side-chat launch, system install,
  Codex accounts, or session `/preview/server` control. These return `404` on
  the web for the hub and for workers. Operator work that needs them uses a
  Railway shell and the loopback API.
- **Previews.** Preview and artifact file routes are proxied. Managed app
  previews that the desktop serves on `*.localhost` origins have no web
  equivalent on this branch.
- **Restarts.** A redeploy or restart of the hub or a worker ends live agent
  processes, terminals, and SSE streams. Only what is on `/data` survives, and
  BFF sessions are lost by design.
- **One operator.** One password and no per-user identity, roles, or second
  factor. OIDC is an option in ADR 0006 and is not built.
- **Off-Railway hosts.** Machines behind NAT (for example the operator's home
  hosts) cannot be reached from the hub until ADR 0008 (RW-5.2) is implemented.

A full sweep of the web build's screens on the live hub is still pending
([#142](https://github.com/petarnenov/ultra-factory/issues/142)).

## Where the code lives

| Area | Path |
| --- | --- |
| Hub image and supervisor | `deploy/railway/hub/` |
| Worker image and entrypoint | `deploy/railway/worker/` |
| Volume backup script | `deploy/railway/scripts/backup-volume.sh` |
| Local Compose e2e | `deploy/railway/compose.e2e.{sh,yaml}`, `frontend/e2e/railway/`, `frontend/playwright.railway.config.ts` |
| BFF | `packages/hub-bff/src/` (`server.ts`, `proxy.ts`, `hosts-store.ts`, `workers-api.ts`, `railway-client.ts`) |
| Railway API notes (RW-3.1.1 spike) | `packages/hub-bff/docs/railway-api-notes.md` |
| Web build switches | `frontend/src/renderer/lib/platform.ts`, `bridge.ts`, `web-settings.ts`, `web-shortcuts.ts` |
| Remote-host listener | `backend/internal/httpd/lan_listener.go`, `auth.go` |
| Report pull | `backend/internal/observe/remotereports/` |
| Hub image CI | `.github/workflows/hub-image.yml`, `.github/workflows/railway-e2e.yml` |
| Backlog and report | `docs/backlog/railway/` |
