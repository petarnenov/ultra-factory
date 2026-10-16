# AO on Railway: orchestrator with a web UI and worker management

**Date:** 2026-10-16. **Base:** `main` @ eb03ffaf6.

## Goal
The orchestrator (AO daemon) and its UI run on Railway and are used from a browser, without the desktop app. From this UI the user adds and removes workers: machines on Railway that run the agent sessions. This report assesses what exists in the code today, which approach fits, how complex it is and how long it will take.

## 1. Conclusions
- **It is not just a deploy.** It requires new code, and two of the components work around hard rules in `AGENTS.md`. They need an ADR.
- **The workers almost exist already.** The "self-hosted remote host" mode is `ao daemon` plus `ao remote-host enable`. It provides an authenticated listener on :3011. `make daemon-dist` was built exactly for a "remote worker host". The CLI can already do `ao spawn --host <worker>`.
- **Three things are missing:**
  - a web version of the UI (today it is Electron only);
  - a server (BFF) in front of it for login, proxy and Bearer;
  - a module that creates and deletes Railway services through the Railway API.
- **Recommendation:** option A (hub + daemon workers), in phases. An MVP with a working worker, visible from the desktop app, after 5–8 days. The full goal is **~25–40 person-days (5–8 weeks for one developer)**.

## 2. What exists in the code today
| Component | State | Where |
|---|---|---|
| Main daemon API | Only `127.0.0.1:3001`, no auth, bind host is not configurable | `backend/internal/config/config.go:20-26` |
| Remote-host listener | `0.0.0.0:3011`, Bearer password or 256-bit account token. Blocks some of the routes | `backend/internal/httpd/lan_listener.go`, `auth.go` |
| Lockout | 5 failed attempts/min per `RemoteAddr`; `X-Forwarded-For` is ignored | `backend/internal/httpd/auth.go:105,144` |
| Worker without cloning the repo | `make daemon-dist` (Node 24 + ACP, no tmux) and `setup-self-hosted.sh --install-only` for containers | `scripts/build-daemon-dist.sh`, `docs/self-hosted-remote.md` |
| Delegation to hosts | `ao spawn/send/session --host`, `ao host ls` | `backend/internal/cli/remote_target.go` |
| Multi-host in the UI | routes `host.$hostId.*`, `clientForHost`, Settings → Remote hosts | `frontend/src/renderer/lib/host-clients.ts` |
| Proxy with injected Bearer | token in the path, because SSE/WS do not send headers (Node code in Electron main) | `frontend/src/main/remote-proxy.ts` |
| Renderer in a browser | `aoBridge` facade with a ready browser stub. `dev:web` is mock data only | `frontend/src/renderer/lib/bridge.ts`, `preview-mode.ts` |
| Base URL | `VITE_AO_API_BASE_URL` is supported, but `applyDaemonStatus` forces `http://127.0.0.1:port` | `lib/api-client.ts:12-26`, `lib/daemon-status.ts:8-11` |
| CSP | `connect-src` is hard-coded to `127.0.0.1` | `frontend/vite.renderer.csp.ts:80-86` |
| AO Cloud control plane | Go + Postgres(RLS) + WorkOS. Starts one sandbox per session; providers are docker/coder/freestyle/createos, no Railway. Deployed on AWS ECS. The hosted web UI is in a private submodule that is not present here | `cloud/`, `cloud/internal/sandbox/provider.go:181` |
| Railway/Fly/k8s configuration | none | — |

## 3. Options

### A. Hub + daemon workers (recommended)
```
Browser ──HTTPS──▶ [Railway: hub]
                    ├─ BFF (Node): login, static SPA, proxy /api /mux SSE, Railway API client
                    └─ ao daemon (orchestrator, 127.0.0.1:3001) + volume
                         │  private network (*.railway.internal:3011, Bearer)
                         ├─▶ [Railway: worker-1] ao daemon + remote-host listener + volume
                         └─▶ [Railway: worker-N] ...
```
- **Worker:** a Railway service from a shared image with git, tmux, node24, gh and the agent CLIs. It has its own volume for `HOME`, `AO_DATA_DIR` and worktrees. The agent keys come in as env (`ANTHROPIC_API_KEY`/`CLAUDE_CODE_OAUTH_TOKEN`, `GH_TOKEN`, …).
- **Adding:** the BFF calls Railway GraphQL: service from image, volume, variables, deploy. Then it waits for `GET /api/v1/identity` to respond and records the host in the registry.
- **Removing:** a warning about active sessions, then deletion of the service and the volume, and removal from the registry.
- **The orchestrator in the hub** delegates to the workers with `ao spawn --host`. This is already implemented.

### B. AO Cloud control plane on Railway + a new Railway sandbox provider
- Workers are automatic, one per session. They are not added manually. This diverges from the request.
- The `docker` provider is not possible: there is no Docker socket, and hosted mode rejects it anyway.
- Hosted mode requires WorkOS and a Postgres role without superuser/BYPASSRLS.
- **There is no UI** in the repo. It would need a new one, or the desktop app with `AO_CLOUD_CONTROL_PLANE_URL`.
- **Estimate:** ~35–60 days for the control plane + provider, plus ~20–40 days for a web UI. I do not recommend it for this goal.

### C. Only one daemon on Railway, accessed from the desktop app
This is phase 1 of A, ~3–5 days. No web UI and no worker management.

## 4. Plan for option A: complexity and time
Estimates are in person-days for a developer who knows the repo.

| # | Work | Complexity | Days |
|---|---|---|---|
| 0 | **ADRs:** (a) the BFF exposes the hub's loopback API behind its own auth; (b) trusting `X-Forwarded-For` behind the Railway proxy | medium (a decision, not code) | 1–2 |
| 1 | **Worker image:** Dockerfile based on `daemon-dist` + tmux + agent CLIs. Entrypoint: `HOME`/`AO_DATA_DIR`/`AO_RUN_FILE` on the volume, pre-seed of `mobile/config.json` (enabled, password, port 3011), `AO_HARNESS_UPDATE_CHECKS=off`, healthcheck. Publish to GHCR | low–medium | 2–3 |
| 2 | **Listener behind a proxy:** (a) lockout: today 5 failed attempts block all clients for a minute, a trusted-proxy mode is needed; (b) dual-stack bind (`[::]`) for IPv6 private networking | medium (security code) | 2–3 |
| 3 | **Manual deploy of 1 worker and connection from the desktop app** via Settings → Remote hosts. Verify private networking (IPv4/IPv6 for the `0.0.0.0` bind), session, terminal, PR | low | 1 |
| 4 | **Web build of the renderer:** web `daemon` adapter (health → ready), base URL without `127.0.0.1`, CSP, hiding Electron-only features (browser panel, editor handoff, CLI link, auto-update), keyboard shortcuts in the renderer, Web Notifications, localStorage for ui settings | medium | 5–8 |
| 5 | **BFF (Node):** login (single user or OIDC) with a session cookie, static SPA, reverse proxy for REST/SSE/`/mux` to the hub daemon and to each worker with Bearer (port of `remote-proxy.ts`), host registry on the volume | medium–high | 5–8 |
| 6 | **Hub image:** daemon + BFF + SPA in one container with two processes under a supervisor | low–medium | 1–2 |
| 7 | **Worker management:** Railway GraphQL client (create/volume/vars/deploy/status/delete), async statuses (provisioning → ready → failed), UI dialog "Add worker" (name, keys, size) and "Remove" with confirmation | medium–high | 5–8 |
| 8 | **Hardening:** secrets, a limit on the number of workers (cost), logs, volume backups, e2e test (Playwright web), documentation | medium | 3–5 |
| | **Total** | | **25–40** |

The backlog for this table (epic → story → task → subtask) is in the `backlog/` directory next to this file.

**Stages:**
- **MVP-1** (phases 0–3, ~5–8 days): workers on Railway, managed from the desktop app.
- **MVP-2** (+4–6, another ~11–18 days): web UI on Railway with manual addition of workers by address.
- **Full goal** (+7–8, another ~8–13 days): adding and removing workers with one click.

## 5. Risks and open questions
1. **Hard rules in `AGENTS.md`.** "Do not add any other network-facing bind", and the main listener is loopback only. A BFF that publishes the loopback API is an architectural decision and needs an ADR. The compromise is for the hub to go through :3011 like the other workers. Then the routes the listener blocks are lost: system install, codex accounts, side-chat launch, preview server.
2. **Security.** Access to the UI equals a shell on all workers. The Railway API token in the hub can create paid resources. A strong login, limits and a project-scoped token are required.
3. **Restart and redeploy kill the live agent processes.** The PTY host and tmux do not survive a container restart. Only the DB and the worktrees on the volume remain. Image updates must be done when the worker is empty.
4. **Agent authentication.** Interactive OAuth login in a container is awkward, so the keys go through env vars. If a subscription is used instead of an API key, the provider's terms must be checked.
5. **Railway specifics, to verify:**
   - **IPv6.** Private networking on Railway is (at least historically) IPv6 only, while the worker's listener binds to `0.0.0.0`, which is IPv4 only (`backend/internal/httpd/lan_listener.go:202`). If that is still the case today, the hub will not reach the workers via `*.railway.internal` without a dual-stack bind (`[::]`). This is the first thing to verify in phase 3;
   - `ao remote-host enable` has no `--port` flag. The port comes only from `lastPort` in `<AO_DATA_DIR>/mobile/config.json`, which `RestoreOnBoot` reads at startup (`controllers/mobile.go:711`). The worker's entrypoint must pre-seed this file;
   - one volume per service, no zero-downtime deploy with a volume;
   - the schema and limits of the Railway public GraphQL API;
   - the size of the image with the agent CLIs.
6. **Features you lose in the browser:** the embedded browser panel, the agent CDP browser, annotations, profile import, editor handoff and `*.localhost` previews.
7. **One daemon per worker, one user.** No multi-tenancy. A team needs option B.

## 6. Recommended next step
Phase 0 and phase 1 + 3: the ADR and one manually deployed worker, connected to the desktop app. It costs ~1 week. It validates Railway networking, the image and the agent keys before investing in the web UI and the BFF.
