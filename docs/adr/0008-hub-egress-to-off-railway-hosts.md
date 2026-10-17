# 8. Hub egress to off-Railway hosts over a tailnet

Date: 2026-10-17
Status: Accepted (human approval: option T1 approved by the user, 2026-10-17)

## Context

The hub runs on Railway. Its daemon and BFF must reach the remote-host listener
(`:3011`, bearer-authenticated) of machines outside Railway, such as the operator's
IMac, Linux and Z600 on a home LAN. Those machines sit behind NAT. The goal is no
inbound port on the home network and no bearer on the public internet.

Constraints found in the code and platform:

- **No route.** Railway private networking only reaches services in the same Railway
  project. Railway containers have no `NET_ADMIN` and no `/dev/net/tun`, so Tailscale
  can only run as `tailscaled --tun=userspace-networking`. That mode exposes the
  tailnet through a loopback SOCKS5 proxy, not a route.
- **The daemon refuses environment proxies on purpose.** `remotehost.DirectClient`
  sets `Proxy = nil` and disables redirects, so a saved bearer never crosses an
  `HTTP(S)_PROXY` (agent sandboxes set one). Any tailnet path must be a narrower,
  explicit exception, not a re-enabled environment proxy.
- **Precedents.** `remotehost.UnattendedTransportAllowed` accepts plain `http://` to
  `100.64.0.0/10` and `*.ts.net`, because WireGuard encrypts the traffic. It does not
  yet cover the IPv6 tailnet range or a trailing-dot name (see Decision item 7).
  [ADR 0004](0004-cloudflare-tunnel-for-remote-mobile-access.md) already supervises
  `cloudflared` for mobile access.
- **The BFF dials directly.** `packages/hub-bff/src/hosts-probe.ts` and
  `workers-api.ts` use `fetch`. `proxy.ts` uses `node:http`/`node:https`, including the
  `/mux` WebSocket upgrade. All three need the same path.
- **Direction of traffic.** [ADR 0005](0005-daemon-pulls-remote-worker-reports.md)
  keeps all traffic hub → host. The hosts never need to reach the hub.
- **Identity before credential.** [ADR 0005](0005-daemon-pulls-remote-worker-reports.md)
  and #93 require that the unauthenticated identity probe and the bearer request run
  on the same connection, so the bearer only goes to the host that answered the probe.
- **Threat model.** [RW-4.1.1](../railway/threat-model.md) records that a bearer on the
  hub volume is plaintext, and that a compromised hub reaches every worker. A new
  path must not make either worse.

## Options

| | **T1: Tailscale userspace in the hub** | **T2: Cloudflare named tunnel per host** | **T3: Tailscale Funnel per host** |
| --- | --- | --- | --- |
| Public surface | None. The hub and hosts accept no inbound connection from the internet. | Every listener is public `https://`, guarded only by the bearer and the lockout. | Same as T2. |
| Code change | Daemon tailnet dialer and BFF tailnet dialer (RW-5.2.2, RW-5.2.3), image and supervisor (RW-5.2.1). | None in the daemon or BFF. Trusted-proxy mode must be configured (ADR 0005-trusted-proxy). | Same as T2. |
| Secrets | `TS_AUTHKEY` (Railway variable). The node state on `/data` holds the node key. | One tunnel credential per host, plus the Cloudflare account. | Funnel enablement in the tailnet, plus the node state. |
| Cost | Tailscale tailnet (the operator's own). No new infrastructure. | Cloudflare account, a domain and DNS. Quick tunnels do not fit a saved host registry (ADR 0004). | Tailnet with Funnel enabled. Plan limits to verify (see open questions). |
| Bearer and TLS | WireGuard between peers. Tailscale's coordination service does not hold the keys. | TLS ends at Cloudflare, which sees the bearer and the traffic (ADR 0004). | TLS ends at Tailscale's edge, so the bearer is visible there. Verify before relying on it. |
| Failure: tailnet or tunnel down | Hub cannot reach any host. Pulls fail and reports stay `pending` on the host (ADR 0005). Nothing is lost. | Cloudflare or the per-host tunnel is down, so that host is unreachable. | Funnel or its edge is down, or disabled by tailnet policy, so that host is unreachable. |
| Failure: key or credential expiry | Auth key expired before first join: no enrolment. Node key expiry: the node drops off the tailnet until re-enrolled. | Tunnel credential revoked: that host is unreachable. | Funnel disabled by policy: that host is unreachable. |
| Same-connection rule (#93) | Not enforced today; best effort. RW-5.2.2 gates dialing: after the probe, the per-pass dialer refuses any new connection, so the bearer can only travel on a probed one (Decision 1, item 8). The SOCKS hop adds one more place where idle connections can close. | Not enforced today. The edge can close idle connections, so the same dial gate is needed. | Same as T2. |
| SSE and `/mux` | Direct TCP through SOCKS. No buffering layer. | Response bodies are buffered by the tunnel, so SSE does not stream (ADR 0004). `/mux` is unaffected. | Not measured. Verify before relying on it. |

**Decision: T1.** It is the only option that adds no public surface and keeps the
bearer end-to-end encrypted between the hub and the host. T2 and T3 both terminate
TLS at a third party, which ADR 0004 already judged unacceptable for source-code
conversations and bearers. T2 and T3 also leave SSE buffered (T2 per ADR 0004), and
the hub's chat and event path depends on SSE. T1 costs code in the daemon and BFF,
which RW-5.2.2 and RW-5.2.3 already budget.

If T1 is rejected, RW-5.2 shrinks to a runbook (`cloudflared` or Funnel per host with
trusted-proxy mode). That path needs its own decision on bearer visibility first.

## Decision details

### 1. The proxy exception

The exception covers the daemon and the BFF. It is the only way a bearer can reach a
tailnet address through the hub.

1. **Configuration.** `AO_REMOTE_TAILNET_SOCKS`, a `host:port` value. The hub
   supervisor sets it only when `TS_AUTHKEY` is set and the tailnet is up. When it is
   unset, behavior is unchanged from today.
2. **Validation.** The host part must be an IP literal in `127.0.0.0/8` or `::1`. Any
   other value is a startup error. The daemon and the BFF both refuse to start, with a
   clear message. A hostname such as `localhost` is rejected too, so the target cannot
   be redirected by name resolution.
3. **Routing.** A destination is a *tailnet destination* if, after lowercasing and
   removing one trailing dot from a hostname, it is:
   - an IP literal in `100.64.0.0/10` or `fd7a:115c:a1e0::/48` (a bracketed IPv6
     literal is parsed first), or
   - a hostname that ends in `.ts.net`.

   Only tailnet destinations use SOCKS5. Every other destination dials directly, unchanged.
   The same predicate decides the scheme rule in item 7, so routing and scheme cannot
   disagree. The decision uses the name as
   written, not the resolved address. Tailnet hostnames are sent to the SOCKS server
   unresolved, so MagicDNS works. Non-tailnet hostnames are never sent to SOCKS.
4. **Environment proxies are never read.** `HTTP_PROXY`, `HTTPS_PROXY`, `ALL_PROXY` and
   `NO_PROXY` are ignored by the daemon and the BFF. `DirectClient` keeps `Proxy = nil`.
   No code may introduce an environment-proxy agent.
5. **No fallback.** If the SOCKS server refuses or is unreachable, a tailnet
   destination fails. It is reported as unreachable, the same as today. It never falls
   back to a direct dial, because that would send the bearer by a path the ACL did not
   allow.
6. **Redirects.** The redirect guard is unchanged. A tailnet host cannot redirect the
   bearer to a public address.
7. **Scheme.** `https://` is allowed everywhere. Plain `http://` is allowed only toward
   loopback, toward a tailnet destination (item 3), or with `AO_REMOTE_REPORTS_ALLOW_HTTP=1`.
   The current `remotehost.UnattendedTransportAllowed` does **not** yet match item 3: it
   accepts `100.64.0.0/10` and `*.ts.net` only. It misses the `fd7a:115c:a1e0::/48` IPv6
   range and a trailing-dot hostname. RW-5.2.2 must change it to call the item 3
   predicate. The check does not depend on `TS_AUTHKEY`, so the widening applies to every
   daemon, desktop included. It allows plain `http://` for those two forms only. Both are
   WireGuard encrypted, the same as the IPv4 range, so the change is safe but not a no-op.
   The BFF's scheme handling is not changed by this ADR (see open questions).
8. **Same connection (new requirement for RW-5.2.2).** The SOCKS dialer is the
   `DialContext` of the one transport that `DirectClient` already returns. The identity
   probe, the pulls and the acknowledgements share that transport, as ADR 0005 requires.
   Today this is best effort only. `observer.go` probes once with `ReadIdentity` and
   then reuses the `http.Client`. Nothing checks the connection: if the keep-alive
   connection closes between the probe and a later request, `net/http` re-dials and
   sends the bearer on a new connection that never answered the probe. A SOCKS hop adds
   another place where idle connections can close, so the gap widens with this change.
   RW-5.2.2 must close it by gating the dial, not by inspecting the request. The gate is
   **always on**, for tailnet and direct dials on every daemon. Closing the gap for direct
   dials is a second change that applies to every daemon, not only to hub installs:
   - Each host's pass gets its own transport whose `DialContext` (the SOCKS or direct
     dialer) is armed for exactly one probe. `ReadIdentity` runs over that transport.
     When the probe returns, the gate closes.
   - After the gate closes, any dial on that transport fails with an error. A request
     that reuses an idle connection keeps working. A request that would need a new
     connection cannot send the bearer, because no unprobed connection can be created.
     `net/http` cannot write a request on a connection it did not dial through the gate.
   - The failure stops the pass for that host. The next pass builds a new transport and
     re-probes. Pulls and acknowledgements are retried, and the delivery record keeps
     them from being delivered twice (ADR 0005).
   - `httptrace.GotConn` is not the mechanism. It returns nothing, and cancelling the
     request from it races with the write. It may be used as a test assertion only.
   - A test in `observe/remotereports` closes the keep-alive connection between the
     probe and a pull, and asserts that the pull fails with no bearer on the wire.

### 2. Secret handling

- **Auth key type.** A Tailscale auth key that is pre-approved, tagged `tag:ao-hub`,
  **non-reusable** and **non-ephemeral**.
  - *Pre-approved* means the hub joins without a human approving it.
  - *Tagged* gives the node a tag-based ACL and keeps it off any user's identity.
  - *Non-reusable* means a leaked key cannot enrol other machines after the first join.
  - *Non-ephemeral* means the node survives restarts. An ephemeral node is removed when
    it goes offline, and its state would be lost.
  - Cost: after a volume is lost, a new key is needed. That is an acceptable operator
    step.
- **Where the key lives.** In the Railway variable `TS_AUTHKEY`, passed only to the
  supervisor. The supervisor removes it from the environment of every child process
  after `tailscale up`. It is never logged, never printed in `ps` output, and never
  written to the volume. The way it reaches `tailscale up` must not be on a command
  line (see open questions).
- **Node state.** `/data/tailscale/` (mode `0700`, owned by uid 10001). It holds the
  node's private key, so it is a new secret on the hub volume, of the same class as the
  bearers in the threat model. RW-5.2.1 adds it to the asset table of the threat model.
- **Rotation.** The auth key is rotated as follows:
  1. Create a new key in the Tailscale admin console with the same tag, non-reusable
     and non-ephemeral.
  2. Replace `TS_AUTHKEY` in Railway and redeploy the hub. The volume keeps the node
     state.
  3. Confirm `tailscale status` shows the hub online with `tag:ao-hub`.
  4. Revoke the old key in the admin console.

  If the node state is lost, step 2 enrols a new node. The old node entry must then be
  removed from the admin console. The exact behavior of an already-enrolled node with a
  new key is an open question.
- **Logs.** The supervisor and the daemon never log the key, the node key or a bearer.

### 3. ACL

The tailnet policy must replace any default allow-all rule for these tags.

- `tag:ao-hub` (the hub) may connect to `tag:ao-host` on the TCP listener ports of the
  hosts (`3011` by default, or the port in each host's `mobile/config.json` `lastPort`).
  No other port is allowed.
- `tag:ao-host` (each machine) has **no** rule that reaches `tag:ao-hub`. Hosts never
  initiate a connection to the hub, which matches ADR 0005.
- The operator's own user devices keep their existing access to the hosts, through
  their own rule.
- The hub runs no `tailscale serve`, no Funnel and no Tailscale SSH. It exposes no
  tailnet service.
- Each host is tagged `tag:ao-host` when it joins (`tailscale up --advertise-tags`).

The host's listener is still `0.0.0.0:3011` (ADR 0007). The ACL, not the bind,
limits who on the tailnet can reach it. The home router must not forward any port to
the hosts. A host firewall that allows `3011` only on the tailscale interface is
recommended, but that is an operations step outside this ADR.

## Consequences

- **New outbound dependency.** The hub depends on the tailnet control plane and on
  Tailscale's coordination service. The coordination service sees node metadata, not
  bearer or payload contents, because WireGuard keys stay on the peers.
- **New loopback listener.** `tailscaled` opens a SOCKS5 listener on `127.0.0.1:1055`
  inside the hub container. It is loopback only, not a network-facing bind, and it is
  the one bind this ADR adds. The `AGENTS.md` rule against other network-facing binds
  is extended to cover it (see below).
- **New secret on the volume.** The node state is a secret on `/data`. It is listed in
  the threat model (RW-5.2.1).
- **Failure mode is stated, not hidden.** A tailnet outage stops pulls and sends. Reports
  stay pending on the hosts and are delivered when the path returns, so nothing is lost
  (ADR 0005).
- **Code scope.** RW-5.2.2 and RW-5.2.3 implement this exception, with tests for
  routing, validation, environment-proxy avoidance and same-connection behavior. The
  SOCKS dialer is active only when `AO_REMOTE_TAILNET_SOCKS` is set. Two changes apply
  to every daemon, with or without it:
  - the same-connection dial gate (item 8), for direct dials too. Its cost is that a
    pass fails and re-probes when a keep-alive connection closes mid-pass. Pulls and
    acknowledgements are retried on the next pass, so no reports are lost, but passes
    may take longer under churn;
  - the scheme widening in Decision item 7 (IPv6 tailnet range and trailing-dot names),
    which is WireGuard encrypted.
- **AGENTS.md.** The hard rule about network-facing binds and the proxy rule are
  extended by this ADR. The reference is added to the AGENTS.md hard rules and takes
  effect with this ADR's acceptance.

## Open questions

These cannot be checked without a Tailscale account or a deployed hub. Each is a
verification step for RW-5.2.1 or RW-5.2.2. None is answered here.

1. **Re-enrolment.** What `tailscale up --authkey` does when the state directory
   already holds a logged-in node. Is a new key ignored, or does it need a logout first?
   This decides the rotation steps above.
2. **Key transport.** Whether the pinned `tailscale` release accepts the key from a file
   (`--authkey=file:<path>`) or from an environment variable. The key must not appear on
   a command line, so the method chosen must be verified.
3. **Expiry.** The maximum lifetime of a tagged auth key, and whether a tagged hub node
   is exempt from node-key expiry. Both must be set so the hub does not drop off the
   tailnet on its own.
4. **Name resolution.** Whether the SOCKS5 server resolves `*.ts.net` (MagicDNS) when the
   hostname is sent unresolved, as RW-5.2.2 requires.
5. **BFF `http://` scope.** Whether the BFF should apply the daemon's plain-http rule to
   non-tailnet hosts. Today it accepts any `http(s)` URL, and "behaves exactly as
   before" without `TS_AUTHKEY` forbids changing that in this task. It needs its own
   decision in RW-5.2.3.
6. **Host firewall.** Whether each host's firewall restricts `3011` to the tailscale
   interface. This is an operations decision, not a code one.
7. **Listener ports.** The exact set of `3011` / `lastPort` values per host, which the
   ACL must list.
8. **T2 and T3 re-check.** The SSE buffering measured in ADR 0004 for T2, and Funnel's
   port and TLS limits for T3. These matter only if T1 is rejected.

## Verification

- Reviewed against the `AGENTS.md` hard rules. The only new bind is the loopback SOCKS5
  listener in the hub, and the proxy exception is narrower than any environment proxy.
- Reviewed against the [RW-4.1.1 threat model](../railway/threat-model.md):
  - Residual 7 (private-network traffic unencrypted) does not apply to the tailnet
    path, which is WireGuard-encrypted.
  - Residual 3 (bearers plaintext on the hub volume) is unchanged. The node state is a
    new item in the same class.
  - The "compromised hub reaches every worker" risk is unchanged. The ACL limits only
    which ports a hub can reach, not what a bearer can do.
