# RW-4.2.1 · Таск: Backup и redeploy стратегия

**Тип:** операции. **Оценка:** 0.5 чд. **Стори:** RW-4.2.

## Контекст
- Рестарт на контейнер убива PTY host/tmux процесите (`spawn_unix.go`, `Setsid`), DB и worktrees оцеляват на volume-а.

## Събтаскове
- [ ] Скрипт `deploy/railway/scripts/backup-volume.sh`: `railway ssh` + `tar` на `/data/ao/ao.db` (с `sqlite3 .backup`) и `hosts.json` към локална машина или S3-съвместим bucket.
- [ ] Redeploy политика: BFF endpoint `POST /hub/workers/:id/drain` → маркира `draining`, UI не позволява нови сесии; когда няма активни → оператор прави redeploy. Документирай.
- [ ] Провери поведението на daemon-а след рестарт за сесии, които са били running: статус, възможност за resume (`docs/architecture.md` lifecycle/reaper).
