# RW-3.3.3 · Таск: Диалог „Премахни работник“

**Тип:** frontend React. **Оценка:** 0.5–1 чд. **Стори:** RW-3.3.

## Събтаскове
- [ ] При отваряне: `GET /hosts/:id/api/v1/sessions` → показва активните сесии; предупреждение, че volume-ът и worktrees ще бъдат изтрити безвъзвратно.
- [ ] Потвърждение чрез въвеждане на label-а; checkbox „Force (kill running sessions)“ за `?force=1`.
- [ ] Submit → `DELETE /hub/workers/:id` → статус removing → изчезва от списъка. 409 → показва сесиите и подсказва force.
- [ ] За `source: manual` диалогът само маха записа и го казва ясно.
- [ ] Vitest.
