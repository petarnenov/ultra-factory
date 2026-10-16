# RW-2.5.2 · Таск: Деплой на hub в Railway и runbook

**Тип:** операции + документация. **Оценка:** 0.5–1 чд. **Стори:** RW-2.5.

## Събтаскове
- [ ] Service `hub` от image, volume на `/data`, variables от RW-2.5.1, публичен домейн, target port = `$PORT`.
- [ ] Генерирай operator password hash локално (`node scripts/hash-password.mjs`) и го сложи като variable; никога plaintext.
- [ ] Добави работника от RW-1.4 през UI (Settings → Remote hosts) с `http://worker-1.railway.internal:3011` (ако IPv6 private networking работи) или публичния му URL.
- [ ] Runbook `deploy/railway/README.md`: раздел „Hub“: деплой, ротация на operator password и session secret, backup на `/data`, ъпдейт на image (когда hub-ът няма активни сесии), логове.
- [ ] Валидационен лог: време за зареждане на SPA, SSE латентност, терминал.

## Критерии за приемане
- [ ] Епик RW-2 критериите са проверени и записани.
