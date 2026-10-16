# RW-3.3.1 · Таск: Списък на работниците

**Тип:** frontend React. **Оценка:** 0.5–1 чд. **Стори:** RW-3.3.

## Събтаскове
- [ ] `components/settings/WorkersSettings.tsx`: таблица/карти с label, статус badge (provisioning с spinner, online, unreachable, auth_failed, removing, failed с tooltip `lastError`), брой активни сесии, created at, source (manual/railway), линк към `host/<id>`.
- [ ] Orphans секция (от RW-3.2.2) с „Adopt“ и „Delete“.
- [ ] Query hook `useHubHosts()` с адаптивен polling.
- [ ] Vitest: рендер на всички статуси.
