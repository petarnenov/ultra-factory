# hub-bff

The web backend-for-frontend for the AO hub on Railway, as decided in
[ADR 0006](../../docs/adr/0006-web-bff-for-hub.md). It is a separate Node
process, not a daemon listener. It is the hub's only public surface: it serves
the web build of the renderer, owns the single-operator login, and proxies to daemons through their authenticated listeners. It never
connects to the daemon's loopback listener on `127.0.0.1:3001`.

This package provides static SPA serving, login, session cookies, CSRF checks,
login throttling, a daemon proxy, and the host registry under `/hub/hosts`.
It also reconciles Railway workers through `/hub/workers` when Railway is configured.

## Railway worker client

`src/railway-client.ts` implements the provider-neutral `WorkerProvider` using
Railway's GraphQL API. Supply the token, token kind, project ID, and environment
ID through `RailwayClientOptions`. Project tokens use the
`Project-Access-Token` header by default; account tokens use `Authorization:
Bearer`. Automated tests use recorded response shapes and a fake fetch. The
manual create-to-running-to-delete integration test remains skipped: the
project token tested in RW-3.1.1 could not delete services or volumes, and no
account/workspace token with cleanup permissions has been authorized or
verified. Do not run the lifecycle against Railway until that permission is
established.

## Running

Node 24 runs the TypeScript sources directly (type stripping), so there is no
build step.

```bash
npm --prefix frontend run build:web                    # → frontend/dist-web
printf '%s' 'the password' | npm --prefix packages/hub-bff run --silent hash-password
AO_HUB_SPA_DIR=frontend/dist-web AO_WEB_PASSWORD_HASH='$scrypt$...' PORT=8080 \
  npm --prefix packages/hub-bff start
npm --prefix packages/hub-bff test                     # vitest + supertest
npm --prefix packages/hub-bff run typecheck
```

| Variable | Required | Meaning |
|---|---|---|
| `AO_HUB_SPA_DIR` | yes | Directory of the web build (`frontend/dist-web`), must contain `index.html`. |
| `AO_WEB_PASSWORD_HASH` | one of | Operator password hash from `npm run hash-password`. Preferred. |
| `AO_HUB_OPERATOR_PASSWORD_HASH` | alias | The hub image's name for `AO_WEB_PASSWORD_HASH`. Set one or the other. |
| `AO_WEB_PASSWORD` | one of | Plaintext operator password. Hashed at start-up, removed from the process env, never logged. |
| `PORT` | no | Listen port. Railway sets it. Default `8080`. |
| `AO_HUB_BFF_HOST` | no | Listen address. Default `::` (all interfaces, dual-stack), which Railway needs. |
| `AO_HUB_DATA_DIR` | no | Directory of `hosts.json`, the host registry, on the hub volume. Default `AO_DATA_DIR`, else `~/.ao/data`. |
| `AO_HUB_DAEMON_PASSWORD_FILE` | no | The hub daemon's `mobile/config.json`, source of the `hub` entry. Default `<AO_DATA_DIR or ~/.ao/data>/mobile/config.json`. |
| `AO_WEB_ORIGIN` | no | Public origin such as `https://hub.example.com`. When set, state-changing requests must carry exactly this `Origin`; otherwise `Origin` must match the request's `Host`. |
| `AO_HUB_MAX_WORKERS` | no | Maximum registered Railway workers. Default `5`; must be a positive integer. |

Rotating the password is changing the variable and redeploying. Sessions live in
memory, so a redeploy also signs everyone out.

## Routes

| Route | Auth | Behavior |
|---|---|---|
| `GET /healthz` | none | `200 {"ok":true}`. Fixed body, touches no daemon. |
| `GET /login`, `GET /_bff/login.{css,js}` | none | Minimal login page served by the BFF itself, with a strict CSP. Redirects to `/` when already signed in. |
| `POST /login` | none | JSON `{"password": "..."}`. `204` + session cookie, `401` on a wrong password, `429` + `Retry-After` when throttled, `503` + `Retry-After` when 2 password checks are already running. |
| `POST /logout` | none | Ends the session and clears the cookie. `204`. |
| `GET /hub/hosts` | session | `{"hosts":[...]}`: every registered host with its last `status`. Never includes a password or token. |
| `POST /hub/hosts` | session | JSON `{"label","url","password"}`. Probes `GET <url>/api/v1/identity`, then checks the bearer with `GET <url>/api/v1/projects`, then saves. `201 {"host":...}`; `400` invalid input; `409` duplicate label or host; `422 host_credential_rejected`; `502 host_unreachable` or `unsupported_api_version`. |
| `DELETE /hub/hosts/:id` | session | Removes the registry record only (nothing in Railway is deleted). `204`; `404`; `409 hub_not_removable` for the hub entry. |
| `POST /hub/hosts/:id/probe` | session | Probes the host now. `200 {"host":...}` with the new `status`; `404`. |
| `GET /hub/workers` | session | Returns registered hosts, detected Railway service orphans, status counts, the last successful reconcile, and the configured worker limit. Does not adopt or delete orphans. |
| `POST /hub/workers` | session | Creates a Railway worker asynchronously. Returns `202`; returns `429 {"error":"worker_limit_reached","count","limit"}` at the configured limit. |
| `DELETE /hub/workers/:id` | session | Requests deletion of a registered Railway worker; failed deletion remains recorded for manual cleanup. |
| everything else | session | The SPA from `AO_HUB_SPA_DIR`, with `index.html` as the fallback for extensionless client-side routes. Without a session: `302 /login` for a page navigation, `401 {"error":"unauthorized"}` otherwise. |

The login page is the BFF's own, not a route in the SPA: ADR 0006 puts the SPA
shell itself behind the session, so the SPA cannot render a login form before
sign-in. The SPA only needs to treat a `401` from the BFF as "go to `/login`".

Worker reconciliation resumes recorded provisioning and removing jobs on BFF
startup, then lists project services every five minutes. It matches the
`ao-worker-` service-name prefix used by this BFF and logs unmatched services as
orphans; it never deletes them. The Railway API example documents listing
services under a project, but does not document a label/tag filter. The BFF
currently has no worker-management screen, and orphan adoption/deletion actions
are not available until a safe workflow is defined.

## Host registry

`<AO_HUB_DATA_DIR>/hosts.json` is a JSON array of host records with the field
names of the desktop's `remotes.json` and the CLI's `remoteEntry`
(`label`, `url`, `password`, `hostId`, `accountUserId`), plus `source`
(`manual`, `railway`, or `hub`), `railway` (`serviceId`, `volumeId`,
`environmentId`), `createdAt`, `status`, and `checkedAt`. It is written
atomically (temporary file + rename) with mode `0600`, and the BFF refuses to
start if it is readable by group or others. A host's `id` in the API is its
`hostId`; the hub entry is also reachable as `hub`.

- **Status** is `unknown` until the first probe, then `online`, `unreachable`,
  or `auth_failed`. Every host is probed at startup and every 30 s. A probe
  calls the unauthenticated identity route, then an authenticated read with
  `X-AO-Expected-Host-ID`. A worker that answers with a different host id is
  `unreachable`, and is sent no bearer. Redirects are never followed.
- **The `hub` entry** is created from the hub daemon's `mobile/config.json` on
  the same volume (ADR 0006, B2): `http://127.0.0.1:<lastPort>` (default
  `3011`), with the account token as the bearer when there is one, else the
  connection password. It is refreshed before every poll, so a rotated
  credential is picked up, and it cannot be deleted. A `lastPort` of `3001`,
  the daemon's unauthenticated listener, is refused.
- **Manual hosts** may not use a loopback address, so the registry can never
  point the proxy at the hub daemon's own listeners.
- **Proxy lookup**: `HostsStore` implements the proxy's `ProxyHosts`:
  `getHub()` and `getHost(id)` return `{url, hostId, bearer}`, and only for an
  entry whose identity is confirmed.
- **URLs** are origins only (`http(s)://host[:port]`), as the proxy requires.

## Security model

- **Session cookie** `ao_hub_session`: a random 256-bit id, `HttpOnly; Secure;
  SameSite=Strict; Path=/`. Sessions are kept in memory with a 7-day sliding
  idle timeout and a 30-day absolute lifetime (the cookie's `Max-Age`). There is
  no signing secret: the id is opaque and only meaningful to this process.
- **CSRF**: besides `SameSite=Strict`, every request other than `GET`/`HEAD`,
  including `POST /login`, must carry an `Origin` that matches the host (or
  `AO_WEB_ORIGIN`), and a `Sec-Fetch-Site`, if sent, must be `same-origin`.
  Otherwise `403`. `POST /login` also requires `Content-Type: application/json`,
  which a cross-site form cannot send.
- **Login throttling**: 5 attempts per minute per client. The client is the
  rightmost valid `X-Forwarded-For` entry, the same rule as
  [ADR 0005](../../docs/adr/0005-trusted-proxy-remote-host.md), because the BFF
  sits behind Railway's edge. An unparseable rightmost entry falls back to the
  TCP peer, never to an earlier entry. The key is used for throttling only.
  Run the BFF only behind a single trusted proxy: a client that reaches the port
  directly can choose its own key.
- **Login memory**: one scrypt check takes ~128 MiB, and the throttle above is
  per client. So at most 2 password checks run at once
  (`MAX_CONCURRENT_PASSWORD_CHECKS`). Beyond that, `POST /login` returns
  `503 {"error":"busy"}` + `Retry-After: 1` without calling scrypt.
- **Logs**: one JSON object per line on stdout. Request lines carry the method,
  the path without the query string, the status, and the duration. Login lines
  carry the client key. Passwords, hashes, cookies, and headers are never logged.

## Decisions

### No framework (`node:http`)

The BFF is built on `node:http` with no runtime dependencies, rather than
`fastify` + `@fastify/http-proxy`:

- It is the hub's only public surface, so every dependency is attack surface and
  supply chain on the one process that guards a shell on every host. Zero
  runtime dependencies keeps that at Node itself.
- The route set is tiny (health, login, logout, static files, and the proxy
  prefixes), so a router buys little.
- The proxy of RW-2.4.2 has a working in-repo precedent on `node:http`:
  `frontend/src/main/remote-proxy.ts` already forwards REST, streams SSE, and
  pipes the `/mux` WebSocket upgrade with an injected bearer. Porting that logic
  is less risk than adapting `@fastify/http-proxy`'s header handling to the
  strip-and-inject rules of ADR 0006.

Dev-only dependencies (`typescript`, `vitest`, `supertest`, `@types/*`) are
pinned to exact versions.

### Password hashing: `node:crypto` scrypt

ADR 0006 allows "a salted scrypt or argon2id hash". The options compared:

| Option | Verdict |
|---|---|
| `argon2` (npm) | Native addon (node-gyp or prebuilt binaries). Adds a native build to the Railway image and a supply-chain dependency for the one secret check. Rejected. |
| `bcryptjs` | Pure JS, but bcrypt truncates passwords at 72 bytes, is not memory-hard, and is not one of the ADR's two named schemes. Rejected. |
| `node:crypto` `argon2` | Present in recent Node 24 releases but still experimental. Not relied on. |
| `node:crypto` `scrypt` | Built in, memory-hard, no dependency. **Chosen.** |

Parameters are OWASP's scrypt baseline, `N=2^17, r=8, p=1` (128 MiB and just under a
second per check on a developer machine, which also slows guessing). The
hash is stored as `$scrypt$ln=17,r=8,p=1$<salt>$<hash>` (unpadded base64url),
so the cost can be raised later without breaking existing hashes. Comparison
uses `timingSafeEqual`.

### Daemon proxy integration

`createBff({ hosts, ... })` accepts the `ProxyHosts` interface from `src/proxy.ts`:
`getHub()` and `getHost(id)` return a paired `{ url, hostId, bearer }` entry or
`undefined`. `main.ts` passes the `HostsStore` (see "Host registry"), which
answers only for paired entries and supplies the automatic hub entry. A host
that is not registered or not paired yet answers 404. No browser-provided URL
is used.

The hub entry must target the authenticated loopback listener (normally
`http://127.0.0.1:3011`), never the primary daemon listener. Port 3001 is rejected.
Both HTTP and HTTPS upstreams are supported; HTTPS uses Node's normal certificate
verification. Upstream URLs must be origins without paths or embedded credentials.

Authenticated requests to `/api/*` (including preview/artifact file routes),
`/readyz`, and `/mux` use the hub. `/hosts/:hostId` prefixes select a registered
host and are removed before forwarding. `/healthz` stays local and public.
WebSocket upgrades additionally require the same Origin check as mutations.

The proxy replaces credentials and the expected host ID, strips browser cookies,
Origin, client IP forwarding headers, and hop-by-hop headers, and never forwards
upstream `Set-Cookie` headers. Preview file requests get an upstream-only `ao_conn`
cookie. SSE and WebSocket streams have no idle timeout and close upstream work
when the browser disconnects. Upstream connection failures and credential
rejections return 502 JSON with a generated `requestId`; a host's 401 never logs
out the browser session. A WebSocket transport error after upgrading closes with
code 1011 where the downstream socket is still writable.

Run `npm run hub-bff:check` from the repository root for the typecheck and fake
upstream tests (REST, SSE, WebSocket, and authentication boundaries).
