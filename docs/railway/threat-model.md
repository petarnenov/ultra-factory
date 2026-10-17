# Railway hub threat model

Status: current for RW-4.1.1, updated for RW-5.4.1. Scope: the hub container
(daemon + web BFF, `packages/hub-bff`, `deploy/railway/hub`) and the Railway worker services it
manages (`deploy/railway/worker`). It covers what exists on this branch. Decisions
that shape it: [ADR 0005](../adr/0005-trusted-proxy-remote-host.md) (trusted proxy),
[ADR 0006](../adr/0006-web-bff-for-hub.md) (web BFF in front of the hub daemon),
[ADR 0007](../adr/0007-dual-stack-remote-host-listener.md) (dual-stack listener).

## Headline

**Access to the UI is a shell on every worker.** The app API spawns agents and
terminals, sends prompts to agents that edit code and run commands, and opens
sessions in worktrees. Whoever holds a valid BFF session, or a host bearer, can run
arbitrary commands on each host that bearer reaches. Every control below exists to
keep that access away from anyone who should not have it. None of them makes a
legitimate session safer than a shell.

## Assets

| Asset | Where it lives | Why it matters |
|---|---|---|
| Operator password hash | Railway variable `AO_WEB_PASSWORD_HASH` (scrypt) | Sole credential for the web UI. A plaintext `AO_WEB_PASSWORD` is hashed at start-up and removed from the process env. |
| BFF session ids | BFF memory only | Each one is a shell on every host. Lost on redeploy. |
| Railway API token | Railway variable `RAILWAY_API_TOKEN` | Creates and deletes services and volumes in the project. Must be project-scoped (ADR 0006). |
| Host bearers | `<AO_HUB_DATA_DIR>/hosts.json` (0600) on the hub volume; `mobile/config.json` for the hub's own bearer | Authenticate the BFF to the hub and every worker. |
| Worker bearers | Worker `mobile/config.json` on each worker volume; mirrored in `hosts.json` | Same as above, from the worker side. |
| Agent credentials | Each host's `~/.ao` and harness config (API keys, Codex accounts) | Used by agents running on that host. |
| Hub orchestrator provider credential (RW-5.4.1) | Railway variable `ANTHROPIC_API_KEY` or `CLAUDE_CODE_OAUTH_TOKEN` on the `hub` service, or an interactive login under `/data/home` (`.claude/.credentials.json`, `.codex/auth.json`) | A leaked key spends the provider account and can run the model outside AO. In the environment it is readable by every agent process in the hub. |
| Hub orchestrator GitHub token (RW-5.4.1) | Railway variable `GH_TOKEN` on the `hub` service | Fine-grained, this repository only: issues, pull requests, contents. A leaked token can comment, open and merge PRs, and push branches in this repository until it expires or is revoked. |
| Code in worktrees | Each host's `AO_DATA_DIR` worktrees and volumes | Source code, unpushed work, and whatever the agents wrote. |
| Volume backups | Railway volume snapshots | Contain every bearer above in plaintext. Protect them like `~/.ao`. |
| Daemon control surface | Loopback `127.0.0.1:3001` (unauthenticated) and `127.0.0.1:3011` (bearer) | `/shutdown`, telemetry, host installers, and the app API. |

## Attackers

1. **Internet attacker, no credentials.** Can reach the Railway edge and therefore the
   BFF's `$PORT`, the login form, and the health check. Target: the password, a
   session, or a host detail.
2. **Credential guesser.** Has the public login URL and unlimited time. Target: the
   operator password.
3. **Browser-borne attacker.** A malicious page the operator visits, or an injected
   script in the SPA. Can make cross-origin requests with the operator's cookie and
   attempt to read the page. Target: state changes or stolen session data.
4. **Compromised worker or agent output.** An agent running on a worker, acting on
   untrusted repository content or a prompt injection, can read that host's
   `~/.ao`, its bearer, and its volume. Target: other hosts through the bearers
   stored on the hub.
5. **Network path attacker.** Between the operator's browser and Railway, or
   between the hub and a worker's private address. Target: cookies, bearers in
   transit.
6. **Insider or a teammate with the Railway project.** Holds the Railway token or
   the volume snapshots. Out of scope for in-app controls; mitigated by
   project scoping and access control in Railway.

## Vectors and mitigations

### Web login and session (attackers 1, 2, 3)

| Vector | Mitigation | Where | Residual risk |
|---|---|---|---|
| Password guessing | scrypt hash, 5 attempts per minute per client (rightmost `X-Forwarded-For`), `503` above 2 concurrent checks | `password.ts`, `rate-limit.ts`, `server.ts` | One strong password is the only factor. The throttle is per client address, and a distributed attacker can spread attempts. |
| Session theft | `HttpOnly; Secure; SameSite=Strict; Path=/` cookie; 256-bit random id | `server.ts` `sessionCookie` | A stolen cookie is a shell until it expires or is revoked. |
| Session lifetime | Idle timeout 7 days, absolute 30 days | `sessions.ts` | A long-lived session on a shared machine stays valid. |
| Logout or password change | `POST /logout` ends the current session. `DELETE /hub/sessions` ends every session (logout everywhere). Rotating the password is a redeploy, which clears the memory store. | `server.ts`, `sessions.ts` | No per-session revoke in the UI. No in-process password change, so rotation depends on a redeploy. |
| Session listing | `GET /hub/sessions` returns timestamps and a `current` flag only, never session ids. | `server.ts` | None known. |
| CSRF | Every state-changing request needs an `Origin` matching the request host (or `AO_WEB_ORIGIN`) and no cross-site `Sec-Fetch-Site`. A missing `Origin` is refused. | `server.ts` `sameOrigin` | Relies on `SameSite=Strict` plus the header check. A non-browser client with a stolen cookie can supply its own `Origin`. |
| Cross-site framing (clickjacking) | `X-Frame-Options: DENY`, `frame-ancestors 'none'` in the SPA CSP. | `server.ts` | None known. |
| Stored or injected script | SPA CSP: `script-src 'self'`, no inline script, `object-src 'none'`, `base-uri 'self'`, `connect-src` limited to same origin plus the PostHog and control-plane origins the web build uses. The header matches the build's CSP meta tag. | `server.ts` `SPA_CSP`, `frontend/vite.renderer.csp.ts` | `style-src 'unsafe-inline'` is kept for the build. An injected script that stays same-origin can still call the API with the session cookie. |
| Transport | Railway terminates TLS. `Strict-Transport-Security: max-age=31536000; includeSubDomains`. | `server.ts` | HSTS takes effect only over HTTPS, which Railway serves. HSTS preload is not used. |
| Browser feature abuse | `Permissions-Policy` denies camera, microphone, geolocation, payment, USB, and motion sensors. `Referrer-Policy: no-referrer` stops URLs leaking to third parties. `X-Content-Type-Options: nosniff`. | `server.ts` | None known. |
| Unauthenticated probing | Every route except `/healthz`, `/login`, and its assets needs a session. Unauthenticated callers get `401 {"error":"unauthorized"}` (or a redirect to `/login` for a page navigation), with no host id, URL, or upstream detail. The BFF does not forward `GET /api/v1/identity` to them. | `server.ts`, `proxy.ts` | None known. Tests pin the bare `401` for `/hub/*`, `/hosts/<id>/*`, and WebSocket upgrades. |
| Path tricks | Reject `..`, `//`, encoded separators, and NUL before URL normalization. | `proxy.ts` `hasSafeRequestPath`, `server.ts` | None known. |

### BFF to daemon and workers (attackers 4, 5)

| Vector | Mitigation | Where | Residual risk |
|---|---|---|---|
| Browser-supplied credentials reach a daemon | The BFF strips `Authorization`, `Cookie`, `Origin`, and forwarding headers from the client and injects the stored bearer per host. The renderer never sees a bearer. | `proxy.ts` `target` | None known. |
| Client chooses its lockout key | The BFF strips `CF-Connecting-IP`, `X-Forwarded-For`, `Forwarded`, and `X-Real-IP` before forwarding. | `proxy.ts` | None known. |
| Reaching the unauthenticated loopback API | The BFF never targets port 3001. `target` refuses a host entry that uses it. | `proxy.ts` | A misconfigured `hosts.json` entry pointing elsewhere is trusted with a bearer. Entries are written by the BFF's own APIs. |
| Upstream error leaks | An upstream `401` maps to `502 host credential rejected`. Upstream bodies and URLs are never returned. `1011` closes WebSockets without detail. | `proxy.ts` | None known. |
| Daemon exposure | The daemon's remote listener is bound to `127.0.0.1:3011` in the hub and is reachable only from inside the container. The primary `127.0.0.1:3001` stays unauthenticated and internal. | `deploy/railway/hub/hub-supervisor.sh`, ADR 0006 | A process inside the container is inside the trust boundary. |
| Plaintext worker traffic | `AO_HUB_WORKER_URL_MODE` selects a worker's public Railway domain or its private network address (`*.railway.internal`). The bearer is required on every request. | `workers-api.ts`, `railway-client.ts`, ADR 0007 | Private-network traffic is not encrypted by the app. Railway's network is the boundary. The public mode relies on Railway's HTTPS. |
| Host identity confusion | Each request carries `X-AO-Expected-Host-ID`. A host entry without a `hostId` is not used until paired. | `proxy.ts`, `hosts-probe.ts` | None known. |

### Stored credentials (attackers 4, 6)

| Vector | Mitigation | Where | Residual risk |
|---|---|---|---|
| Reading `hosts.json` | File mode 0600, the BFF refuses to start if group or other can read it, and writes are atomic. Responses never include a `password`. | `hosts-store.ts`, `hosts-api.ts` | Plaintext bearers on the volume. Anyone with volume access or a snapshot has every bearer. |
| Reading the Railway token | Passed as an environment variable, never logged, never returned. Must be project-scoped. | `config.ts`, `railway-client.ts` | A project-scoped token can still create or delete any service in the project. |
| Logs | The BFF logs decoded paths only, never query strings, headers, bodies, passwords, hashes, or session ids. Tests pin this. | `server.ts`, `log.ts` | Paths can still contain host ids. |
| Operator password in env | Hashed at start-up and removed from `process.env`. | `config.ts`, `main.ts` | The hash is still in the process environment. |

### Worker and agent behavior (attacker 4)

| Vector | Mitigation | Where | Residual risk |
|---|---|---|---|
| Agent runs untrusted repository code | Worktrees are per session. Agents run as an unprivileged user (uid 10001) in the container. | `deploy/railway/hub/hub-supervisor.sh`, `deploy/railway/worker/ao-worker-entrypoint.sh` | An agent with a shell can read its host's `~/.ao` and bearer. Containment is per host, not per agent. |
| Prompt injection through issues, PRs, or docs | Agents act with the permissions of their harness. The app does not grant extra authority to agent output. | Harness configuration | Prompt injection is not solved. Treat agent output as untrusted input to the next step. |
| Cross-host spread | The hub holds each worker's bearer. A worker holds only its own. | ADR 0006 | A compromised hub is a compromise of every worker it reaches. |
| Agent in the hub exfiltrates the orchestrator's credentials | Credentials come from Railway variables or the CLI's own login file, never from the repository, the image, or a log. `GH_TOKEN` is a fine-grained token for this repository only, without admin or account permissions, with an expiry; it cannot bypass branch protection. `gh auth login` is not used, so the token is not copied to `hosts.yml`. Rotation and revocation: `deploy/railway/README.md`, "Orchestrator agent credentials". | Railway variables, `deploy/railway/README.md` | Any agent in the hub, including one acting on a prompt injection, can read the environment and `/data/home`. Containment is the token's scope and the provider's spend limits, not the app. |

## Residual risk

These items remain after RW-4.1.1 and RW-5.4.1. Each needs its own task or decision.

1. **Access to the UI is a shell on every worker** (see the headline). The web UI is
   as powerful as the desktop app for the hosts it reaches. Reducing this needs a
   permission model in the daemon, not only a login.
2. **One password, one operator.** No second factor, no per-user identity, no
   per-session revoke in the UI. OIDC is an option in ADR 0006 and is not built.
3. **Bearers are plaintext on the hub volume** (`hosts.json`, worker
   `mobile/config.json`). Encryption at rest would need a key that is not on the
   same volume.
4. **Sessions are memory-only.** A redeploy signs everyone out, which is a feature
   for password rotation, but there is no audit trail of sessions.
5. **No rate limit on authenticated API use.** A valid session can issue requests
   as fast as the daemon accepts them.
6. **`style-src 'unsafe-inline'` in the CSP.** It is required by the build today.
7. **Private-network traffic is unencrypted** inside Railway's network.
8. **Login throttle is per client address.** A distributed guesser is limited only by
   the scrypt cost and the concurrency cap, not by the per-client limit.
9. **Volume snapshots contain every bearer.** Backup handling is an operator
   procedure (RW-4.2.1 covers backup and redeploy).
10. **Agent prompt injection** is not mitigated by the app.
11. **Orchestrator credentials in the hub (RW-5.4.1).** The provider key and
    `GH_TOKEN` are shared by every agent process in the hub, and an interactive
    login file is in every volume snapshot. A leak is limited by the
    fine-grained token's scope and expiry and by the provider's spend limits;
    detection is the provider's and GitHub's audit logs.

## Verification

- `packages/hub-bff/test/security.test.ts`: the security headers on every response
  class, the SPA CSP (including equality with the web build's CSP meta tag), the
  unauthenticated `401` without host details, session listing without ids, logout
  everywhere, CSRF on `DELETE /hub/sessions`, and rejection of a session across a
  password rotation.
- `packages/hub-bff/test/server.test.ts` and `proxy.test.ts`: login, throttling,
  CSRF, idle timeout, credential injection, and the `502` mapping of an upstream `401`.
- Hub supervisor: `deploy/railway/hub/supervisor_test.sh`.
