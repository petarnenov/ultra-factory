# RW-3.2.2 · Таск: Reconcile, сираци и лимити

**Тип:** Node TS. **Оценка:** 1–1.5 чд. **Стори:** RW-3.2. **Зависимости:** RW-3.2.1.

## Събтаскове
- [ ] При старт на BFF-а: за всеки host с `status ∈ {provisioning, removing}` продължи job-а (poll или delete).
- [ ] Периодично (5 мин): `provider.list()` на services с label/tag `ao-worker=1` в project-а; service без запис в `hosts.json` → лог WARNING „orphan“ + показване в UI с бутон „Adopt“/„Delete“. Никога автоматично триене.
- [ ] `AO_HUB_MAX_WORKERS` (default 5): `POST /hub/workers` → 429 при достигане, с брой и лимит в тялото.
- [ ] Метрики в лог: брой работници по статус, последен reconcile.
- [ ] Тестове: orphan detection, resume след рестарт, лимит.
