# RW-2.4.1 · Task: BFF skeleton with login and session cookie
**Status:** ✅ done (#73)

**Type:** Node TS (new package). **Estimate:** 1.5–2 pd. **Story:** RW-2.4.

## Context
- New package `packages/hub-bff/` (or `deploy/railway/hub/bff/`), Node 24, TypeScript, no framework or with `fastify` (it has `@fastify/http-proxy` with WebSocket support). Pick one and record it in the package README.
- Precedent for a proxy with a token: `frontend/src/main/remote-proxy.ts` (token in the path, because EventSource/WS do not send headers). The BFF instead uses a cookie, which the browser sends for both SSE and WS.
- Login model from ADR RW-1.1.2: a single operator, `AO_HUB_OPERATOR_PASSWORD_HASH` (argon2id/bcrypt) in Railway variables; `AO_HUB_SESSION_SECRET` for signing.

## Subtasks
- [ ] `package.json`, `tsconfig`, add the package to `NPM_DIRS` in the Makefile and to root workspaces if there are any.
- [ ] `GET /healthz` (no auth), `GET /` and static files from `dist-web/` (the path comes from `AO_HUB_SPA_DIR`).
- [ ] `POST /login` (JSON `{password}`), `POST /logout`; cookie `ao_hub_session`: `HttpOnly; Secure; SameSite=Strict; Path=/`, a signed JWT or an opaque id in memory + a volume file; TTL 7 days, sliding.
- [ ] Rate limit on `/login`: 5/min by `X-Forwarded-For` (Railway edge).
- [ ] CSRF: `SameSite=Strict` + an `Origin`/`Sec-Fetch-Site` check for all non-GET requests.
- [ ] A minimal login page (it can be part of the SPA, a `/login` route that calls the BFF; if the SPA handles login, the BFF returns a 401 JSON and the SPA redirects).
- [ ] JSON logs on stdout; no credentials in the logs.
- [ ] Tests (vitest + supertest): 401 without a cookie, login ok/fail, rate limit, CSRF rejection.

## Acceptance criteria
- [ ] `npm --prefix packages/hub-bff run test` green; `npm run typecheck` green.
