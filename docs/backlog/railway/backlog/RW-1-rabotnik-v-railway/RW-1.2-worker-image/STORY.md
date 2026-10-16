# RW-1.2 · Story: Worker image and entrypoint

**As** an operator **I want** a published container image that, when started on Railway, brings up `ao daemon` with the remote-host listener enabled and all of its state on a volume, **so that** I can create a worker with just an image URL and env variables.

**Estimate:** 2–3 pd. **Dependencies:** none for the code; RW-1.3 for full operation behind a proxy. **Blocks:** RW-1.4, RW-3.

| Task | Name | pd |
|---|---|---|
| RW-1.2.1 | Worker Dockerfile | 1 |
| RW-1.2.2 | Entrypoint: volume, listener pre-seed, credentials from env | 1 |
| RW-1.2.3 | CI: build and publish to GHCR | 0.5–1 |

**Acceptance criteria**
- [ ] `docker run -e AO_REMOTE_PASSWORD=... -v vol:/data ghcr.io/<org>/ao-worker:<tag>` gives `GET :3011/api/v1/identity` → 200 with `hostId`.
- [ ] `ao remote-host status` inside the container shows Enabled, the address, and the password.
- [ ] After a restart, the hostId, password, DB, and worktrees are the same.
- [ ] The image contains everything `systemcheck` marks as Required.
