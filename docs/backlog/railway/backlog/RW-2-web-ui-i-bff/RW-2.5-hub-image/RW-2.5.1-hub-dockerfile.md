# RW-2.5.1 · Таск: Dockerfile на hub-а и supervisor

**Тип:** инфраструктура. **Оценка:** 1 чд. **Стори:** RW-2.5.

## Събтаскове
- [ ] `deploy/railway/hub/Dockerfile`: `FROM ghcr.io/<org>/ao-worker:<tag>` (или същия multi-stage с ARG), добавя `packages/hub-bff` build и `frontend/dist-web`.
- [ ] Supervisor: `tini` + малък shell/Node скрипт или `s6-overlay`, който пуска `ao daemon` (entrypoint от RW-1.2.2, с `AO_REMOTE_BIND` не е нужен, listener-ът слуша loopback: `LoopbackOnly=true` в pre-seed) и BFF-а на `$PORT`. При смърт на един процес → изход на контейнера (Railway рестартира).
- [ ] BFF env: `AO_HUB_SPA_DIR`, `AO_HUB_DATA_DIR=/data/hub`, `AO_HUB_OPERATOR_PASSWORD_HASH`, `AO_HUB_SESSION_SECRET`, `AO_HUB_DAEMON_URL=http://127.0.0.1:3011`, `AO_HUB_DAEMON_PASSWORD_FILE=/data/ao/mobile/config.json`.
- [ ] `EXPOSE $PORT` (Railway задава `PORT`), `HEALTHCHECK` към BFF `/healthz`, който проверява и daemon `/readyz`.
- [ ] CI job в `worker-image.yml` или отделен `hub-image.yml` за публикуване.

## Критерии за приемане
- [ ] `docker run -p 8080:8080 -e PORT=8080 -v vol:/data ao-hub:dev` → login → board.
