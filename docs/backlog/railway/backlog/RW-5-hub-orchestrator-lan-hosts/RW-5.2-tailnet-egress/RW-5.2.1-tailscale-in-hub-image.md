# RW-5.2.1 · Task: Tailscale userspace in the hub image and supervisor
**Status:** ⬜ todo

**Type:** infrastructure. **Estimate:** 1 pd. **Story:** RW-5.2.

## Goal
The hub container joins the operator's tailnet when `TS_AUTHKEY` is set and exposes it only as a loopback SOCKS5 proxy.

## Context
- `deploy/railway/hub/Dockerfile` (FROM the worker image) and `deploy/railway/hub/hub-supervisor.sh` (tini; when any child dies the container exits).
- The processes run as uid 10001 after setup, and `/data` is the volume (mode `0700`).
- Railway provides no `/dev/net/tun`, so `--tun=userspace-networking` is the only mode.

## Subtasks
- [ ] Install a pinned `tailscale`/`tailscaled` release in the hub image (checksum-verified, no `curl | sh`).
- [ ] Supervisor: if `TS_AUTHKEY` is set, start `tailscaled --tun=userspace-networking --socks5-server=127.0.0.1:1055 --state=/data/tailscale/tailscaled.state --socket=/data/tailscale/tailscaled.sock` as uid 10001, then `tailscale up --authkey=… --hostname=${TS_HOSTNAME:-ao-hub}`. Clear the key from the environment of every other child. If `tailscaled` dies, the container exits.
- [ ] Export `AO_REMOTE_TAILNET_SOCKS=127.0.0.1:1055` to the daemon and the BFF only when the tailnet is up.
- [ ] Without `TS_AUTHKEY`, nothing about the hub changes.
- [ ] Extend `deploy/railway/hub/supervisor_test.sh` for both modes. Document `TS_AUTHKEY`/`TS_HOSTNAME` in `deploy/railway/README.md` (Hub variables), including key rotation.

## Acceptance criteria
- [ ] In a deployed hub, `tailscale status` shows the hub online with the expected tag. `/data/tailscale` is `0700` and owned by uid 10001.
- [ ] The auth key never appears in logs, in `ps` output of children, or on the volume.

## Verification
- `bash deploy/railway/hub/supervisor_test.sh`. Local `docker run` with a test tailnet key: `curl --socks5-hostname 127.0.0.1:1055 http://<tailnet-ip>:<port>/api/v1/identity` returns 200 from inside the container.

## Dependencies
RW-5.1.1.
