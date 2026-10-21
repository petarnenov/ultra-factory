# 11. Remote hosts discover and authenticate each other through Tailscale

Date: 2026-10-21
Status: Accepted

## Context

Remote hosts (`ao spawn --host`, Settings → Remote hosts) depended on AO Cloud:
Settings needed an AO Cloud sign-in, hosts published their address to the
account, desktops copied addresses from it, and passwords or account tokens
authenticated every call. In practice the control plane rejected Tailscale
`http://100.x` addresses, machines never linked from another desktop never
appeared, and stale account rows lingered. The user runs Tailscale on every
machine and does not want AO Cloud in this path (#494).

The authenticated remote listener of [0001](0001-lan-listener-for-mobile.md)
binds `0.0.0.0` behind the bearer-password `authMiddleware`; only
`GET /api/v1/identity` is unauthenticated ([0003](0003-unauthenticated-identity-probe.md)).

## Decision

1. **Discovery runs in the daemon** (`backend/internal/tailnet`). It reads the
   peer list from the local `tailscaled` (`tailscale status --json`) every 30 s
   and probes `GET /api/v1/identity` on each online peer at its Tailscale IPv4
   address on the well-known port `3011` (`AO_TAILNET_PORT`) plus any
   `AO_TAILNET_PROBE_PORTS`. A host is identified by its `hostId`; its URL is
   always the Tailscale address. This install and its sibling installs
   (`~/.ao/data`, `~/.ao/dev/data`) are never listed. The directory is served
   on the loopback API only (`/api/v1/tailnet/*`, blocked on the remote
   listener) to the desktop and `ao host ls`; hidden hosts and label overrides
   live in `<data dir>/tailnet/hosts.json`.
2. **The remote listener gains a Tailscale-only bind mode**
   (`LANManager.StartTailnet`): `ao remote-host enable` binds the existing
   authenticated listener to this machine's Tailscale address, not `0.0.0.0`.
   `--lan`, `--tunnel`, and `--tunnel-only` keep the previous binds.
3. **Tailnet callers are admitted on identity, not a password.** For a request
   from a Tailscale source address, `authMiddleware` asks the local `tailscaled`
   (`tailscale whois --json`) who the caller is. Untagged nodes of this
   machine's own Tailscale user are allowed; other users and tagged devices
   are rejected unless `AO_TAILNET_ALLOW_USERS` / `AO_TAILNET_ALLOW_TAGS`
   permit them. WhoIs failure, an unknown address, or a non-tailnet source on a
   Tailscale-only bind is rejected (403 `TAILSCALE_UNAVAILABLE`,
   `TAILNET_FORBIDDEN`, `TAILNET_REQUIRED`). The identity probe stays exempt;
   the `X-AO-Expected-Host-ID` guard still applies after auth. On a
   Tailscale-only bind the password is accepted only with
   `--allow-password`; on the LAN bind the password path is unchanged, so
   Connect Mobile keeps working and a same-user phone on the tailnet is also
   admitted by identity.
4. **AO Cloud is optional.** The address publisher runs only with
   `AO_CLOUD_REMOTE_HOST_SYNC=1`; the desktop imports account hosts only when
   the user is signed in and turned **Sync hosts through AO Cloud** on.
   Settings → Remote hosts needs no sign-in. Existing `~/.ao/remotes.json`
   entries keep working: a discovered `hostId` moves to its Tailscale address
   with no password, entries for this machine or its siblings are removed, and
   an entry whose address now answers as this machine is forgotten on connect.

## Consequences

- No AO Cloud request is made for remote hosts unless cloud sync is on;
  `TestRemoteHostPublisherIsOffByDefault` pins that.
- Trust is delegated to Tailscale: whoever controls a node of the same
  Tailscale user can use every host with remote access on, which is the
  single-user model the user asked for. Multi-user tailnets need the allowlist.
- A host that could not bind the well-known port falls back to an ephemeral
  port and is not discovered unless that port is listed in
  `AO_TAILNET_PROBE_PORTS` on the discovering machine. The daemon logs the
  fallback.
- Discovery is one `tailscale status` plus one short identity probe per online
  peer per interval; WhoIs is one `tailscale whois` per authenticated request,
  with this machine's own login cached for a minute.
- The `tailscale` CLI is taken from `PATH`, falling back to the macOS app
  bundle (`/Applications/Tailscale.app/Contents/MacOS/Tailscale`); without
  either, discovery reports **Tailscale unavailable** and the listener rejects
  tailnet callers.
