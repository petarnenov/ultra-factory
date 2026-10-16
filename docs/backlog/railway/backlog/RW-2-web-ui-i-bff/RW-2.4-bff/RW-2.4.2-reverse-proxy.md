# RW-2.4.2 · Task: Reverse proxy for REST, SSE and `/mux`

**Type:** Node TS. **Estimate:** 2–3 pd. **Story:** RW-2.4. **Dependencies:** RW-2.4.1, RW-1.1.2.

## Context
- Per ADR B2: the hub daemon is simply a host `hub` in the registry with URL `http://127.0.0.1:3011` and the password from `mobile/config.json` on the same volume. With B1: URL `http://127.0.0.1:3001` without a Bearer. The proxy code is the same; only the entry differs.
- Daemon CORS (`backend/internal/httpd/cors.go:28-82`): requests without `Origin` pass; with a disallowed `Origin` → 403. The BFF **must strip the `Origin` header** (or set it to `app://renderer`) when forwarding, otherwise the browser Origin will be rejected. Precedent: `remote-proxy.ts:160-167`.
- `/mux` is a WebSocket (`terminal_mux.go`), SSE is a long-lived response: disable buffering and timeouts for these paths.
- Blocked LAN routes (`lan_listener.go:67-78`) will return 404 through :3011; the SPA must treat them as "not available" (RW-2.2).

## Subtasks
- [ ] Routes: `/api/*`, `/mux`, `/healthz`→(BFF own), `/readyz` → host `hub`; `/hosts/:hostId/api/*`, `/hosts/:hostId/mux`, `/hosts/:hostId/readyz` → host by id from the registry.
- [ ] For every forwarded request: add `Authorization: Bearer <password|accountToken>`, `X-AO-Expected-Host-ID: <hostId>`, remove `Cookie` and `Origin`, keep `Accept`, `Content-Type`, `Last-Event-ID`.
- [ ] SSE: `Content-Type: text/event-stream`, flush on every chunk, `X-Accel-Buffering: no`, no idle timeout; when the client closes, abort the upstream.
- [ ] WebSocket upgrade for `/mux` and `/hosts/:id/mux` with a Bearer to the upstream; bidirectional pipe; close with a code on error.
- [ ] Preview/artifact paths (`/preview/files/` uses the `ao_conn` cookie upstream): forward this case too, with the BFF setting the cookie for the upstream, not the browser.
- [ ] Errors: upstream unreachable → 502 JSON with `requestId`; 401 from upstream → 502 "host credential rejected" (do not pass the 401 through to the browser, so it does not get logged out).
- [ ] Tests: a fake upstream (http server) for REST, an SSE stream with 3 events, WS echo; check that `Origin` and `Cookie` do not reach the upstream, while `Authorization` does.

## Acceptance criteria
- [ ] The renderer in the web build works fully through the BFF against a local `ao daemon` with remote-host enabled (`npm run dev:web:real` with `AO_DEV_API_TARGET=http://127.0.0.1:<bff>`).
