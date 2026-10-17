# RW-5.2.2 · Task: Daemon tailnet dialer for remote hosts
**Status:** ✅ done (#148)

**Type:** backend. **Estimate:** 1–1.5 pd. **Story:** RW-5.2.

## Goal
`ao spawn --host`, the other `--host` commands and the remote report puller reach tailnet hosts through the hub's loopback SOCKS5. All other destinations keep dialing directly.

## Context
- `backend/internal/remotehost/remotehost.go`: `DirectClient` (sets `Proxy = nil`, disables redirects), `UnattendedTransportAllowed` (already accepts `100.64.0.0/10` and `*.ts.net`), `ReadIdentity`, `DrainBody` (#93).
- Callers: `backend/internal/cli/remote_target.go` and `backend/internal/observe/remotereports/observer.go`. The report puller keeps one client per host through settle, so acknowledgements reuse the connection that answered the identity probe (#93, ADR 0005).
- The exception is fixed by the RW-5.1.1 ADR. Never honor `HTTP(S)_PROXY`/`ALL_PROXY`.

## Subtasks
- [ ] Read `AO_REMOTE_TAILNET_SOCKS` once. Reject any value that is not a loopback `host:port` with a clear startup error.
- [ ] In `DirectClient`, when it is set, use a `DialContext` that routes through SOCKS5 (`golang.org/x/net/proxy` or a minimal in-repo dialer) only for destinations in `100.64.0.0/10`, `fd7a:115c:a1e0::/48` or `*.ts.net`. Pass hostnames unresolved, so MagicDNS works. Keep `Proxy = nil` and the redirect guard.
- [ ] Keep `ReadIdentity` and the bearer request on the same TCP connection through the SOCKS path (keep-alive plus `DrainBody`).
- [ ] Unit tests: the tailnet destination goes through a fake SOCKS5 server; a non-tailnet destination never touches it; an environment proxy is still ignored; a non-loopback value is rejected.
- [ ] An integration test in `observe/remotereports` proves exactly-once delivery and one connection per pass through the SOCKS path.

## Acceptance criteria
- [ ] With the variable unset, behavior and tests are unchanged.
- [ ] With it set, `ao host ls`, `ao spawn --host` and the report pull work against a tailnet host from inside the hub.

## Verification
- `cd backend && go test -race ./internal/remotehost/... ./internal/observe/remotereports/... ./internal/cli/...`, then `make check`.

## Dependencies
RW-5.1.1, RW-5.2.1 (for live verification only).
