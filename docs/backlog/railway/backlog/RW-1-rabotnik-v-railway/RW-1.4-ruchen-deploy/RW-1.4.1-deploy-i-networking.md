# RW-1.4.1 · Таск: Деплой в Railway и проверки на networking

**Тип:** операции. **Оценка:** 0.5 чд. **Стори:** RW-1.4.

## Събтаскове
- [ ] Railway project `ao-lab`; service `worker-1` от image `ghcr.io/<org>/ao-worker:<tag>`; volume монтиран на `/data`; variables: `AO_REMOTE_PASSWORD`, `AO_REMOTE_BIND=dual`, `AO_REMOTE_TRUSTED_PROXY=1`, `ANTHROPIC_API_KEY` (или OAuth token), `GH_TOKEN`, `AO_GIT_USER_*`; target port 3011; публичен домейн.
- [ ] Публичен URL: `curl https://<domain>/api/v1/identity` → 200; грешна парола ×5 от един клиент → 429/lockout само за него (с втори клиент през друг IP или проверка в логовете).
- [ ] Private networking: втори временен service (`curlimages/curl`) → `curl http://worker-1.railway.internal:3011/api/v1/identity`. Запиши дали резолвва IPv6 и дали отговаря. Ако IPv4-only bind не работи, това потвърждава нуждата от RW-1.3.2.
- [ ] `railway redeploy` → провери hostId и паролата непроменени, DB запазена.
- [ ] Измери: време от deploy до `/readyz`, размер на image-а в Railway UI, памет при idle.
- [ ] Запиши резултатите в `deploy/railway/README.md` раздел „Validation log <дата>“.

## Критерии за приемане
- [ ] Всички горни проверки имат записан резултат (pass/fail + бележка).
