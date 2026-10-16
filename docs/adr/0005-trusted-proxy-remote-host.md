# 5. Trusting a reverse proxy's client address on the remote-host listener

Date: 2026-10-16
Status: Proposed

## Context

The authenticated remote-host listener (the Connect Mobile LAN listener of
[0001](0001-lan-listener-for-mobile.md), enabled by `ao remote-host enable`)
throttles password guessing per source. `authMiddleware` keys the lockout on
`sourceKey(r)`, the host part of `RemoteAddr`, and blocks that key for a minute
after five failures (`backend/internal/httpd/auth.go`, `lockout`).

The listener reads exactly one forwarding header today. When the TCP peer is
loopback, which is how `cloudflared` reaches it under
[0004](0004-cloudflare-tunnel-for-remote-mobile-access.md), a single
`CF-Connecting-IP` value replaces the peer for the lockout key only. A LAN client
cannot use that header, because its peer is never loopback. `X-Forwarded-For` and
`X-Real-IP` are not read anywhere.

A worker daemon hosted on Railway and reached through a public Railway domain sits
behind Railway's edge, a TLS-terminating HTTP proxy. Every request then arrives
from an edge address, not from the client. With the current keying, all clients
share a handful of lockout keys: one attacker sending five wrong passwords locks
out the operator for a minute, repeatedly. That is a denial of service, not a
bypass, but it makes a publicly reachable worker unusable under trivial abuse.

Two facts about Railway's edge are needed to make this safe and **cannot be
verified without a Railway account**. They are open questions for the manual
deployment in RW-1.4 (see "Open questions" below):

- which header carries the client address, and whether the edge **overwrites**
  it or **appends** to a client-supplied value;
- whether the edge connects from a predictable address range that can be told
  apart from other peers, such as private-network peers in the same project.

## Decision

Add an **opt-in trusted-proxy mode** for the remote-host listener, enabled only
by an environment variable on the daemon:

```
AO_REMOTE_TRUSTED_PROXY=1
```

It is **never on by default**, it has no UI or CLI toggle, and it does not change
the loopback listener at all.

When the mode is on, `authMiddleware` derives the lockout key as follows:

1. If the peer is loopback and a single valid `CF-Connecting-IP` is present, use
   it. This is today's cloudflared rule, unchanged and evaluated first.
2. Otherwise, if `X-Forwarded-For` is present, take the **rightmost** valid IP
   across all `X-Forwarded-For` header lines, which is the entry the nearest proxy
   appended. Exactly one trusted hop is assumed. The leftmost entry is
   client-controlled when a proxy appends, so it is never used.
3. Otherwise, or if the value does not parse as an IP, use `RemoteAddr` as today.

The derived key is used for the lockout and nothing else. It is never used for
authorization, for `localControlRequest`, for the LAN control-route block, or to
decide that a request is local. The bearer password or account token is still
required on every request except the `GET /api/v1/identity` probe of
[0003](0003-unauthenticated-identity-probe.md).

The per-IP lockout stays. Two alternatives were considered and rejected:

- **Lockout per credential instead of per IP.** The listener has one connection
  password and one account token. Locking the credential lets any stranger lock
  out the operator, which is exactly the failure this ADR exists to remove.
- **A global failure ceiling.** It has the same lock-out-everyone property.
  If RW-1.4 shows that guessing pressure needs a ceiling, it should be a separate
  decision that rate-limits rather than locks.

This ADR deliberately adjusts the backlog proposal of "use the first
`X-Forwarded-For` entry". If Railway turns out to overwrite the header with a
single value, rightmost and first are the same. If it appends, the first entry is
attacker-chosen, and using it would let one client rotate lockout keys freely and
guess without limit.

### Why the header cannot be spoofed in the intended deployment

The guarantee is about who can reach the socket, the same argument
`lanControlBlock` makes:

- A worker deployed for this mode has **no TCP proxy and no direct public port**.
  The only public path to it is the Railway domain, so every public request
  traverses the edge, and the edge's own appended entry is the rightmost one.
- A client can put anything into the `X-Forwarded-For` it sends. With one trusted
  hop, that content is to the **left** of the edge's entry and is ignored.

**Private networking is a separate case.** Other services in the same Railway
project reach the worker over `*.railway.internal` without passing the edge.
Their requests reach the socket directly and can carry any `X-Forwarded-For`
they like. Under this mode such a peer can choose its own lockout key and so is
not throttled. The peers on that network are the operator's own services, such as
the hub of [0006](0006-web-bff-for-hub.md), so this is accepted for the first
release. If RW-1.4 shows the edge arrives from a stable, documented range, a
follow-up may add `AO_REMOTE_TRUSTED_PROXY_CIDRS` to honor the header only from
that range. This ADR does not add it.

### When the flag is safe and when it is dangerous

| Deployment | Flag | Why |
|---|---|---|
| Worker reached only through a Railway public domain (or a comparable single TLS-terminating proxy that is the sole public path) | Safe, recommended | Clients cannot reach the socket except through the proxy |
| Worker reached only over Railway private networking, no public domain | Leave off | No proxy is involved. `RemoteAddr` is already the real peer |
| Desktop or home machine using Connect Mobile on a LAN, Tailscale, or the managed cloudflared tunnel | **Dangerous, never set** | Any LAN device can send `X-Forwarded-For` straight to the socket and evade the lockout. The tunnel is already covered by the loopback `CF-Connecting-IP` rule |
| Worker behind two or more proxies, or reachable both directly and through a proxy | Dangerous | One trusted hop no longer identifies the client, or direct callers can choose their key |

The flag is honored only through the daemon environment, so a desktop user cannot
switch it on by accident from the UI. The documentation for Railway workers is the
only place that tells anyone to set it.

### What is logged

- **At listener start**, when the mode is on: one `Warn` line stating that
  trusted-proxy mode is enabled and that `X-Forwarded-For` is trusted for lockout
  keys. This makes a misconfigured desktop visible in `ao` logs.
- **When a key becomes locked out**: one `Warn` line with the derived key, the TCP
  peer, and whether the key came from `X-Forwarded-For`, `CF-Connecting-IP`, or
  the peer. Only on the transition into lockout, not per failed request, so a
  flood does not flood the log.
- **Never logged**: the bearer token, the connection password, the account token,
  the cookie, or the raw `X-Forwarded-For` chain beyond the chosen entry.
- Connect-telemetry (`mobileConnectReporter`) keeps receiving the TCP peer, as it
  does today. It is not switched to the forwarded address.

## Consequences

- A publicly exposed Railway worker can no longer be locked for every client by a
  single attacker. Brute-force protection stays per client.
- `authMiddleware` gains one environment-dependent branch. It needs table tests
  for: flag off ignores `X-Forwarded-For`; flag on uses the rightmost entry; a
  spoofed leftmost entry does not change the key; several header lines; an
  invalid value falls back to the peer; the loopback `CF-Connecting-IP` rule still
  wins; the identity probe is still exempt ahead of the lockout.
- Operators carry a configuration risk: setting the flag where clients reach the
  socket directly disables brute-force throttling for those clients. The opt-in
  env var, the start-up warning, and the table above are the mitigations.
- No new route, bind, or authentication path is added. The AGENTS.md hard rules
  for the loopback listener and the LAN listener are unchanged apart from a
  pointer to this ADR.

## Open questions for RW-1.4

To be answered on a real Railway deployment before RW-1.3 ships the code:

1. Which of `X-Forwarded-For`, `X-Real-IP`, or another header does the edge set,
   and does it overwrite or append to a client-supplied `X-Forwarded-For`? Test
   by sending `X-Forwarded-For: 203.0.113.7` through the public domain and
   recording what the daemon receives.
2. Is exactly one proxy hop visible to the container, or does Railway chain
   several?
3. What `RemoteAddr` does the container see for edge traffic, and is it a stable
   range distinguishable from private-network peers?

If question 1 shows that the edge appends and is not the nearest hop, or that it
forwards client headers untouched, this ADR must be revised before
implementation.
