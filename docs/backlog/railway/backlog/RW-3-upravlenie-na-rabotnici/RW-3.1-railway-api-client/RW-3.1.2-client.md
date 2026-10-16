# RW-3.1.2 · Таск: `railway-client.ts`

**Тип:** Node TS. **Оценка:** 1–1.5 чд. **Стори:** RW-3.1. **Зависимости:** RW-3.1.1.

## Събтаскове
- [ ] Интерфейс `WorkerProvider` (по аналогия с `cloud/internal/sandbox/provider.go:181`): `create(spec) → {serviceId, volumeId}`, `getStatus(serviceId) → provisioning|running|failed|stopped`, `setVariables`, `redeploy`, `delete(serviceId, volumeId)`. Railway е първата имплементация; интерфейсът позволява бъдещ друг provider.
- [ ] GraphQL заявки като константи, `fetch` с `Authorization: Bearer $AO_HUB_RAILWAY_TOKEN`, retry с backoff при 429/5xx, timeout 30 s.
- [ ] Spec: `{name, image, env: Record<string,string>, volumeMountPath: "/data", volumeSizeGb?, region?}`.
- [ ] Грешки: типизирани (`RailwayAuthError`, `RailwayQuotaError`, `RailwayNotFound`), без изтичане на токена в съобщенията.
- [ ] Тестове с fixtures от RW-3.1.1 (msw или fake fetch): успех, 429 retry, 401, несъществуващ service.

## Критерии за приемане
- [ ] Интеграционен тест (ръчен, `RAILWAY_TOKEN` env, skip в CI): create → status running → delete върху `ao-lab`.
