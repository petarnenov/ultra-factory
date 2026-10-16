# RW-4.3.1 · Таск: Локален compose и Playwright e2e

**Тип:** тестове. **Оценка:** 1 чд. **Стори:** RW-4.3.

## Контекст
- Прецеденти: `cloud/compose.yaml`, `frontend/playwright.config.ts`, `test/e2e-pod/`, `cloud/test/Dockerfile.worker-smoke` (fake `claude`).

## Събтаскове
- [ ] `deploy/railway/compose.e2e.yaml`: `hub` (image от RW-2.5.1), `worker-1` (worker image с fake `claude` скрипт като в worker-smoke), `FakeRailwayProvider` в BFF-а (`AO_HUB_PROVIDER=fake`), който „създава“ работник като връща адреса на `worker-1`.
- [ ] Playwright спецове: login; добавяне на работник по адрес; създаване на работник през fake provider → online; отваряне на `host/<id>`; клониране на малко локално git репо (fixture); стартиране на сесия с fake агент; терминал показва output; премахване с потвърждение.
- [ ] CI job `railway-e2e.yml` (Docker), пускан на PR, които пипат `deploy/railway/**`, `packages/hub-bff/**`, `frontend/src/renderer/**`.

## Критерии за приемане
- [ ] e2e минава локално и в CI за < 10 мин.
