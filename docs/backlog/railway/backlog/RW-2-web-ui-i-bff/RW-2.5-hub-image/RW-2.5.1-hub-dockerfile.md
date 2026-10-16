# RW-2.5.1 · Task: Hub Dockerfile and supervisor

**Type:** infrastructure. **Estimate:** 1 pd. **Story:** RW-2.5.

## Subtasks
- [ ] `deploy/railway/hub/Dockerfile`: `FROM ghcr.io/<org>/ao-worker:<tag>` (or the same multi-stage with an ARG), adds the `packages/hub-bff` build and `frontend/dist-web`.
- [ ] Supervisor: `tini` + a small shell/Node script or `s6-overlay`, which starts `ao daemon` (entrypoint from RW-1.2.2; `AO_REMOTE_BIND` is not needed, the listener listens on loopback: `LoopbackOnly=true` in the pre-seed) and the BFF on `$PORT`. When either process dies → the container exits (Railway restarts it).
- [ ] BFF env: `AO_HUB_SPA_DIR`, `AO_HUB_DATA_DIR=/data/hub`, `AO_HUB_OPERATOR_PASSWORD_HASH`, `AO_HUB_SESSION_SECRET`, `AO_HUB_DAEMON_URL=http://127.0.0.1:3011`, `AO_HUB_DAEMON_PASSWORD_FILE=/data/ao/mobile/config.json`.
- [ ] `EXPOSE $PORT` (Railway sets `PORT`), `HEALTHCHECK` against the BFF `/healthz`, which also checks the daemon `/readyz`.
- [ ] A CI job in `worker-image.yml` or a separate `hub-image.yml` for publishing.

## Acceptance criteria
- [ ] `docker run -p 8080:8080 -e PORT=8080 -v vol:/data ao-hub:dev` → login → board.
