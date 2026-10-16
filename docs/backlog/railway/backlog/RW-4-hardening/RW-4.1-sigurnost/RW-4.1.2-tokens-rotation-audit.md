# RW-4.1.2 · Таск: Scope на Railway токена, ротация, одит лог

**Тип:** операции + Node TS. **Оценка:** 0.5–1 чд. **Стори:** RW-4.1.

## Събтаскове
- [ ] Ползвай project-scoped Railway токен (от RW-3.1.1), документирай минималните права; откажи стартиране ако токенът е account-wide (ако API-то позволява да се различи).
- [ ] Ротация: runbook за operator password, session secret, Railway токен, пароли на работници (`ao remote-host` regenerate през `/hosts/:id/api/v1/...` ако има маршрут, иначе redeploy с нов `AO_REMOTE_PASSWORD` и update в registry).
- [ ] Одит лог (append-only JSONL на volume): login ok/fail, host add/remove, worker create/delete, force kill. Без секрети.
- [ ] `GET /hub/audit?limit=` за UI (по желание).
