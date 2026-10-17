# RW-5.1.1 · Task: ADR for hub egress to off-Railway hosts
**Status:** ⛔ needs-human (#126)

**Type:** ADR. **Estimate:** 0.5–1 pd. **Story:** RW-5.1.

## Goal
Decide how the hub's daemon and BFF reach the remote-host listeners of machines outside Railway, with no inbound port on the home network and no plaintext bearer on the internet.

## Context
- Railway containers run without `NET_ADMIN` or `/dev/net/tun`. Tailscale can only run as `tailscaled --tun=userspace-networking`, which offers a SOCKS5 and/or HTTP proxy on loopback, not a route.
- `remotehost.DirectClient` deliberately sets `Proxy = nil` so the bearer never crosses an environment proxy (agent sandboxes set one). Any tailnet egress needs an explicit, narrower exception, not a re-enabled environment proxy.
- `remotehost.UnattendedTransportAllowed` already treats `100.64.0.0/10` and `*.ts.net` as encrypted, and ADR 0004 already supervises `cloudflared` for mobile access. Both are precedents.
- The BFF (`hosts-probe.ts`, `workers-api.ts`, `proxy.ts`) must use the same path, including the `/mux` WebSocket upgrade.
- ADR 0005 (the daemon pulls reports) keeps all traffic hub → host. The hosts never need to reach the hub.

## Options
- **T1 (recommended): Tailscale userspace in the hub.** The hub joins the operator's tailnet with an auth key, and its state lives on `/data`. The daemon and the BFF dial tailnet destinations only (`100.64.0.0/10`, `*.ts.net`) through `tailscaled`'s loopback SOCKS5, and everything else directly. No public surface is added. The cost is code changes in the daemon and the BFF (RW-5.2.2, RW-5.2.3) plus one secret (`TS_AUTHKEY`).
- **T2: Cloudflare named tunnel per host.** Each machine runs `cloudflared` with a stable hostname, and the hub dials public `https://` directly. No code change is needed in the daemon or the BFF. The cost: every listener becomes internet-facing behind password and lockout only, a Cloudflare account and DNS are required, and trusted-proxy mode (ADR 0005-trusted-proxy) must be configured for the forwarded client IP. Quick tunnels (ADR 0004) change hostname on restart and do not fit a saved host registry.
- **T3: Tailscale Funnel per host.** Public HTTPS like T2, using Tailscale instead of Cloudflare. The trade-offs are the same as T2.

## Subtasks
- [ ] Compare T1–T3 on public surface, code change, secrets, cost, failure modes (tailnet down, key expiry), and how each keeps `ReadIdentity` on the same connection as the bearer request (#93).
- [ ] For T1, specify the exception: an explicit loopback SOCKS5 address (e.g. `AO_REMOTE_TAILNET_SOCKS=127.0.0.1:1055`), used only for tailnet destinations, never read from `HTTP(S)_PROXY`. A non-loopback value is a startup error.
- [ ] Specify the auth key type (reusable, tagged, pre-approved, non-ephemeral), the ACL (hub tag may reach only the hosts' listener ports), and the key rotation procedure.
- [ ] Write `docs/adr/0008-hub-egress-to-off-railway-hosts.md` and link it from the `AGENTS.md` hard rules where it extends them.

## Acceptance criteria
- [ ] The ADR fixes the option, the exact proxy exception, the secret handling and the ACL. RW-5.2 can be implemented without new decisions.

## Verification
- Review against `AGENTS.md` hard rules and the RW-4.1.1 threat model.

## Dependencies
RW-4.1.1.
