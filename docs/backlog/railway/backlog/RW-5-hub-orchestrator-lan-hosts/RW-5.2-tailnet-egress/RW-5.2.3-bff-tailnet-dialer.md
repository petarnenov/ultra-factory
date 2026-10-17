# RW-5.2.3 · Task: BFF tailnet dialer for probe, workers API and proxy
**Status:** ⬜ todo

**Type:** BFF. **Estimate:** 1–1.5 pd. **Story:** RW-5.2.

## Goal
The BFF reaches tailnet hosts for the identity probe, the workers API and the reverse proxy (REST, SSE and the `/mux` WebSocket upgrade) through the same loopback SOCKS5 as the daemon.

## Context
- `packages/hub-bff/src/hosts-probe.ts` and `workers-api.ts` use global `fetch` with `redirect: "manual"`.
- `packages/hub-bff/src/proxy.ts` uses `node:http`/`node:https` `request` and handles `upgrade` for `/mux`.
- `packages/hub-bff/src/hosts-store.ts` `normalizeHostUrl` already accepts any non-loopback `http(s)://` URL.
- The exception rule (tailnet destinations only, loopback proxy only, environment proxies ignored) comes from the RW-5.1.1 ADR.

## Subtasks
- [ ] Add one dialer module: when `AO_REMOTE_TAILNET_SOCKS` is set and the destination is a tailnet address or `*.ts.net`, connect through SOCKS5 (a pinned, audited dependency such as `socks`, or a minimal in-repo client). Otherwise connect directly.
- [ ] Use it from `proxy.ts` (`createConnection`/agent for HTTP and the upgrade path), and from the probe and workers API (an undici `Agent` with a custom `connect`, or switch them to `node:http`).
- [ ] Keep `redirect: "manual"`, header stripping, and the expected-host-ID guard unchanged.
- [ ] Tests with a fake SOCKS5 server: probe, a REST request, an SSE stream, and a `/mux` upgrade through the proxy. A non-tailnet host bypasses it.

## Acceptance criteria
- [ ] Adding a tailnet host in the hub UI shows it online, and the board, chat and terminal work against it.
- [ ] With the variable unset, behavior and tests are unchanged.

## Verification
- `npm --prefix packages/hub-bff test` and `npm --prefix packages/hub-bff run typecheck`.

## Dependencies
RW-5.1.1, RW-5.2.1 (for live verification only).
