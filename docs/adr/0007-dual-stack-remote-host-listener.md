# 7. The remote-host listener is already dual-stack on Linux

Date: 2026-10-16
Status: Accepted

## Context

The authenticated remote-host listener of [0001](0001-lan-listener-for-mobile.md)
is started by `LANManager.Start` with `net.Listen("tcp", "0.0.0.0:<port>")`
(`backend/internal/httpd/lan_listener.go`). Railway private networking has, at
least historically, resolved `*.railway.internal` to IPv6 addresses only. The
Railway backlog assumed that a `0.0.0.0` bind is IPv4-only, so a hub could not
reach a worker over private networking, and proposed binding `[::]`, either
always or behind an opt-in `AO_REMOTE_BIND=dual`.

That assumption does not hold for Go. For the network `"tcp"` (not `"tcp4"`),
Go's `favoriteAddrFamily` (`net/ipsock_posix.go`) treats **any** wildcard listen
address, `0.0.0.0` included, as a request for a dual-stack socket when the kernel
supports IPv4-mapped addresses. It opens an `AF_INET6` socket and explicitly sets
`IPV6_V6ONLY=0` on it (`net/sockopt_linux.go`). Because the option is set per
socket, the `net.ipv6.bindv6only` sysctl does not affect it, so a Debian or trixie
container image behaves the same as any other Linux host.

Measured on Linux with Go 1.27.1 (the repo toolchain), `bindv6only=0`:

| Listen address | Socket | `IPV6_V6ONLY` | Dial `127.0.0.1` | Dial `::1` |
|---|---|---|---|---|
| `0.0.0.0:0` | `AF_INET6`, reported as `[::]` | 0 | accepted, peer `127.0.0.1` | accepted, peer `[::1]` |
| `[::]:0` | `AF_INET6`, reported as `[::]` | 0 | accepted, peer `127.0.0.1` | accepted, peer `[::1]` |
| `:0` | `AF_INET6`, reported as `[::]` | 0 | accepted, peer `127.0.0.1` | accepted, peer `[::1]` |

The three forms open an identical socket. IPv4 clients appear with a plain IPv4
`RemoteAddr` (Go unmaps `::ffff:a.b.c.d`), so lockout keys and the loopback check
in `authMiddleware` are unaffected.

## Decision

**Do not change the bind address and do not add `AO_REMOTE_BIND`.**
`LANManager.Start` keeps `0.0.0.0`. On Linux, which is the Railway case, that
already accepts IPv4 and IPv6. Switching to `[::]` would open the same socket,
and an opt-in env var would control nothing.

`LANManager.StartLoopback` (`127.0.0.1`, used for tunnel-only mode and for the hub
in [0006](0006-web-bff-for-hub.md)) is not a wildcard and stays IPv4 loopback only.
That is intended.

RW-1.3.2 is therefore reduced to:

1. A regression test in `backend/internal/httpd` that starts the LAN listener
   through `LANManager.Start` and completes an authenticated request over `[::1]`,
   skipped when the host has no IPv6 loopback. This pins the behavior so that a
   future change to `"tcp4"` or to an explicit IPv4 address breaks a test instead
   of silently breaking Railway private networking.
2. A one-line comment at the `Start` call explaining that `0.0.0.0` with `"tcp"`
   is dual-stack in Go and must stay that way for IPv6 private networks.

If the container has IPv6 disabled entirely, Go falls back to an `AF_INET`
socket and the listener stays IPv4-only. That is correct: there is no IPv6 to
serve.

## Consequences

- No behavior change for anyone. LAN, Tailscale, tunnel, and Railway users get
  exactly the socket they get today.
- **The existing listener has always been reachable over IPv6 on Linux** (and
  very likely on macOS and Windows, where Go uses the same dual-stack path; not
  measured for this ADR). The "home-network-only" framing of
  [0001](0001-lan-listener-for-mobile.md) assumed NAT-ed IPv4. A host with a
  global IPv6 address and no inbound firewall exposes the authenticated listener
  to the internet whenever Connect Mobile is on. Authentication and the per-source
  lockout still apply, and plaintext transport is already an accepted risk of
  0001, but the reach is wider than 0001 describes. This ADR records the fact. It
  does not change it, and narrowing the bind is a separate decision.
- `netiface.go` (`backend/internal/mobilebridge`) is unaffected. It advertises only
  private IPv4 and Tailscale IPv4 addresses for pairing, and that stays correct:
  no IPv6 address is advertised to phones, and a Railway hub addresses workers by
  their `*.railway.internal` hostname, not by a pairing address. Advertising IPv6
  candidates is out of scope.
- The listener on a Railway worker answers on both families, so the hub reaches
  it whether private networking resolves to IPv6, IPv4, or both.

## Open questions for RW-1.4

1. Confirm on a real Railway deployment that `http://<worker>.railway.internal:3011`
   reaches a worker whose remote-host listener uses the unchanged `0.0.0.0`
   bind. If it does not, capture the resolved addresses and the socket state
   (`ss -ltnp` in the container) and revise this ADR.
2. Record whether `*.railway.internal` resolves to IPv6 only, IPv4 only, or
   both in the environment used. This ADR does not depend on the answer, but the
   worker documentation should state it.
