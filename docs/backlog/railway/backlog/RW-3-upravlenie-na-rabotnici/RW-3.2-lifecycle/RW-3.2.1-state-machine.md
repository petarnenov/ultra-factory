# RW-3.2.1 · Таск: State machine и persisted jobs

**Тип:** Node TS. **Оценка:** 1–1.5 чд. **Стори:** RW-3.2.

## Събтаскове
- [ ] Разшири host записа: `status: provisioning|online|unreachable|auth_failed|removing|failed`, `railway: {serviceId, volumeId, environmentId}`, `lastError`, `desired: running|removed` (по аналогия с `desired_state` в cloud reconciler-а).
- [ ] `POST /hub/workers` `{label, size?, env: {ANTHROPIC_API_KEY?, CLAUDE_CODE_OAUTH_TOKEN?, GH_TOKEN, AO_GIT_USER_NAME, AO_GIT_USER_EMAIL}}`: генерира `AO_REMOTE_PASSWORD` (32 символа, не 8), записва host със status provisioning **преди** да извика Railway (write-ahead), после `provider.create`, записва id-тата.
- [ ] Poll: status running → probe `GET <url>/api/v1/identity` до hostId → записва hostId, status online. Timeout 10 мин → failed + `lastError`, service остава за диагностика (не се трие автоматично).
- [ ] `DELETE /hub/workers/:id`: ако има активни сесии (`GET /hosts/:id/api/v1/sessions` с филтър running) → 409 с списъка, освен `?force=1`; иначе status removing → `provider.delete` → махане от registry.
- [ ] URL на работника: `http://<service-name>.railway.internal:3011` ако RW-1.4.1 потвърди private networking; иначе публичен домейн, създаден от provider-а.
- [ ] Тестове с fake provider: happy path, Railway грешка при create, timeout, delete с активни сесии.

## Критерии за приемане
- [ ] Всички преходи са покрити с тестове; `hosts.json` никога не губи запис при crash (write-ahead + atomic rename).
