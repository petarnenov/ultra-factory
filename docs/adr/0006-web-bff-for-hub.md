# 6. A web BFF in front of the hub daemon

Date: 2026-10-16
Status: Proposed

## Context

AO is planned to run on Railway as a **hub**: one container with the
orchestrating daemon, plus a web build of the renderer, used from a browser
instead of the desktop app. Worker daemons run as separate Railway services and
are reached the way the desktop reaches remote hosts today: through the
authenticated remote-host listener on `:3011` with a bearer credential.

A browser cannot talk to a daemon directly:

- the primary listener is `127.0.0.1:3001`, unauthenticated by design, and the
  AGENTS.md hard rules forbid changing its bind host or adding auth to it;
- `EventSource` and `WebSocket` cannot set an `Authorization` header, so even the
  authenticated listener cannot be used from a page without a credential in the
  URL;
- AGENTS.md forbids any other network-facing bind in the daemon.

The desktop solves the same problem in `frontend/src/main/remote-proxy.ts`: a
trusted process on the same machine holds the credential in memory and injects
`Authorization: Bearer` on every forwarded REST, SSE, and `/mux` request. The
renderer never sees the credential.

The web UI needs the same kind of trusted process: a **backend-for-frontend
(BFF)**, a small Node server in the hub container that serves the static SPA,
owns the browser login, and proxies to daemons. The open question is which hub
daemon listener the BFF talks to.

**B1. Proxy the loopback listener.** The BFF forwards to `127.0.0.1:3001`. The
daemon is untouched, and every route is available, including those the
remote-host listener blocks. The BFF login becomes the only thing standing between
the internet and the unauthenticated, all-powerful loopback API, including
`/shutdown`, telemetry, Connect Mobile control, and host installers.

**B2. Treat the hub like any worker.** The hub daemon also enables its
authenticated listener, and the BFF talks to it with a bearer credential, the same
path it uses for every worker. The loopback listener stays reachable only from
processes inside the container.

### Routes the authenticated listener does not serve

`lanControlBlock` (`backend/internal/httpd/lan_listener.go`) answers 404 for these
before authentication, on the physical socket, regardless of headers:

| Route | Feature | B1 (loopback) | B2 (`:3011` + Bearer) |
|---|---|---|---|
| `/shutdown` | Stop the daemon | Reachable | Blocked |
| `/internal/*` | Telemetry | Reachable | Blocked |
| `/api/v1/mobile/*` | Connect Mobile control: pairing, password rotation, tunnel | Reachable | Blocked |
| `/api/v1/dev/*` | Developer maintenance (for example project import) | Reachable | Blocked |
| `/api/v1/browser/*` | Built-in browser panel and agent CDP browser | Reachable | Blocked |
| `/api/v1/desktop/*` | Desktop-only session workspace and artifact views | Reachable | Blocked |
| `/api/v1/side-chats/launch/*` | Side-chat launch handoff | Reachable | Blocked |
| `/api/v1/system/install/*` and `POST /api/v1/agents/{target}/install` for non-agent targets | Host-mutating installers | Reachable | Blocked |
| `/api/v1/agents/codex/accounts/*`, `/api/v1/agents/codex/account-switches/*` | Personal Codex account management | Reachable | Blocked |
| `/api/v1/sessions/{id}/preview/server` | Starting a session preview dev server | Reachable | Blocked |
| Everything else in the app API: projects, sessions, spawn/send/kill, conversations, terminals over `/mux`, SSE events, PRs, read-only Codex models, harness install for agent targets | Core supervision | Reachable | Reachable |
| `GET /api/v1/identity` | Host identity probe ([0003](0003-unauthenticated-identity-probe.md)) | Reachable | Reachable without credential |

## Decision

**Adopt B2.** The BFF reaches the hub daemon exactly as it reaches a worker:
through the authenticated listener with a bearer credential.

- The hub daemon enables the authenticated listener in its existing
  **loopback-only** mode (`LANManager.StartLoopback`, restored on boot from
  `"loopbackOnly": true` with `"noPublicTunnel": true` in
  `<AO_DATA_DIR>/mobile/config.json`, so no managed tunnel is started).
  The hub therefore adds **no network bind in the daemon**: the authenticated
  socket is `127.0.0.1:3011`, and only the BFF in the same container uses it.
  The exact pre-seeded file is specified in RW-2.4, which reuses the worker
  entrypoint approach of RW-1.2.
- The BFF never connects to `127.0.0.1:3001`. A test in RW-2.4 must pin this,
  for example by asserting that the configured hub upstream port is the
  authenticated one.
- The daemon code is unchanged by this ADR.

The table above is the list of features **not available in the web UI** under
B2: daemon shutdown, telemetry, Connect Mobile control, developer maintenance,
the built-in browser panel and CDP browser, desktop workspace and artifact
views, side-chat launch, host installers, Codex account management, and session
preview dev servers. Most of these are already Electron-only, and the rest
(installers, account management, Connect Mobile, shutdown) are host
administration that belongs in the container image and environment, not in a
browser tab. The web build hides the corresponding UI (RW-2.x).

B1 is rejected. It would turn a browser login into the sole guard of the
unauthenticated loopback API, which is a direct conflict with the hard rule that
keeps that listener safe "because the OS guarantees nothing off-box can reach
it". B1 should be reconsidered only if the list above proves unacceptable, and
then through its own ADR.

### The BFF is the one new public bind, and it is not the daemon

The BFF listens on Railway's `$PORT` and is the hub's only public surface. It is
a separate process, not a daemon listener, so the "no other network-facing bind"
rule for the daemon still holds. The BFF follows the same discipline as the
daemon's authenticated listener:

- **Unauthenticated routes are limited to:** the login page and its static
  assets, `POST` login, and a health check that returns a fixed body and touches
  no daemon. Everything else, including the SPA shell after login, the API proxy,
  SSE, and `/mux`, requires a valid session.
- The BFF does **not** forward the daemon's `GET /api/v1/identity` to
  unauthenticated callers.
- The BFF strips any client-supplied `Authorization` header and cookie before
  forwarding and injects the stored bearer for the target host, as
  `remote-proxy.ts` does.

### Login model

MVP: **one operator, one password, one session cookie.**

- The operator password is provided as a Railway variable
  (`AO_WEB_PASSWORD_HASH`, a salted scrypt or argon2id hash; a plaintext
  `AO_WEB_PASSWORD` is hashed at start-up and never logged). There is no
  sign-up, no user list, and no password reset flow: rotating the variable and
  redeploying is the reset.
- A successful login issues a random 256-bit session id in a cookie with
  `HttpOnly`, `Secure`, `SameSite=Strict`, `Path=/`. Sessions are kept in BFF
  memory with an idle timeout and an absolute lifetime, so a redeploy logs
  everyone out.
- State-changing requests require a matching `Origin` header in addition to the
  `SameSite=Strict` cookie.
- Login attempts are throttled per client address using the same
  rightmost-`X-Forwarded-For` rule as [0005](0005-trusted-proxy-remote-host.md),
  since the BFF sits behind the same Railway edge.

**OIDC is an option for later**, not part of the MVP: an operator who sets
`AO_WEB_OIDC_ISSUER`, client id, and secret, plus an allow-list of subjects or
emails, gets an OIDC login in place of the password. The session cookie model
above is unchanged. AO remains single-user: OIDC narrows who can be the one
operator. It does not add multi-tenancy.

### Where host credentials live

The BFF keeps the hosts it proxies to in **`hosts.json` on the hub volume**, under
the hub's AO data dir (`<AO_DATA_DIR>/hosts.json`, so `~/.ao/hosts.json` by
default), mode `0600`, written atomically. Each entry uses the field names of
the desktop's `remotes.json` (`RemoteEntry` in `frontend/src/main/remotes-store.ts`)
so the same record can move between them:

```json
[
  { "label": "hub", "url": "http://127.0.0.1:3011", "password": "<bearer>", "hostId": "h_..." },
  { "label": "worker-1", "url": "http://worker-1.railway.internal:3011", "password": "<bearer>", "hostId": "h_..." }
]
```

- `password` holds whatever bearer the host accepts: the connection password or,
  preferably, the account token.
- An entry without `hostId` is not used until the BFF has paired it with
  `GET /api/v1/identity`, matching the desktop's rule that a saved password is
  only sent to a confirmed host.
- The BFF refuses to start if `hosts.json` is readable by group or others, as the
  desktop does for `remotes.json`.
- Credentials never reach the browser. The SPA addresses hosts by `hostId`, and
  the BFF resolves the upstream URL and bearer.

## Consequences

- One code path for all hosts. The hub is "worker zero" to the BFF, and the
  multi-host routing that already exists in the renderer (`host.$hostId.*`,
  `clientForHost`) maps directly onto BFF routes.
- No change to the daemon and no relaxation of the loopback hard rule. The
  loopback-gated control routes stay unreachable from the internet even if the
  BFF login is broken, because the BFF never talks to the loopback listener.
- The web UI is deliberately smaller than the desktop app. The list above is the
  contract; adding any of those features to the web means revisiting this ADR.
- A compromised BFF session is still equivalent to a shell on every host: the
  app API spawns agents and terminals. The login, cookie flags, and throttling
  above are therefore load-bearing, and the Railway API token of RW-3 must be
  project-scoped.
- `hosts.json` on the hub volume is a credential store for every worker. Volume
  backups must be protected like `~/.ao`, as already noted for `identity.json` in
  [0003](0003-unauthenticated-identity-probe.md).
- RW-2.4 can be implemented from this ADR: upstream selection, auth model, cookie
  flags, credential storage and format, and the unavailable-feature list are
  fixed here.
