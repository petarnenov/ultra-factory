# RW-2.4.1 · Таск: Скелет на BFF с login и session cookie

**Тип:** Node TS (нов пакет). **Оценка:** 1.5–2 чд. **Стори:** RW-2.4.

## Контекст
- Нов пакет `packages/hub-bff/` (или `deploy/railway/hub/bff/`), Node 24, TypeScript, без framework или с `fastify` (има `@fastify/http-proxy` с WebSocket поддръжка). Избери едно и го фиксирай в README на пакета.
- Прецедент за proxy с token: `frontend/src/main/remote-proxy.ts` (token в пътя, защото EventSource/WS не пращат хедъри). BFF-ът вместо това ползва cookie, която браузърът праща и за SSE и за WS.
- Login модел от ADR RW-1.1.2: един оператор, `AO_HUB_OPERATOR_PASSWORD_HASH` (argon2id/bcrypt) в Railway variables; `AO_HUB_SESSION_SECRET` за подписване.

## Събтаскове
- [ ] `package.json`, `tsconfig`, добави пакета в `NPM_DIRS` на Makefile и в root workspaces ако има.
- [ ] `GET /healthz` (без auth), `GET /` и статични файлове от `dist-web/` (пътят идва от `AO_HUB_SPA_DIR`).
- [ ] `POST /login` (JSON `{password}`), `POST /logout`; cookie `ao_hub_session`: `HttpOnly; Secure; SameSite=Strict; Path=/`, подписан JWT или opaque id в памет + volume файл; TTL 7 дни, sliding.
- [ ] Rate limit на `/login`: 5/мин по `X-Forwarded-For` (Railway edge).
- [ ] CSRF: `SameSite=Strict` + проверка `Origin`/`Sec-Fetch-Site` за всички не-GET заявки.
- [ ] Минимална login страница (може да е част от SPA-то, маршрут `/login`, който вика BFF-а; ако SPA-то поема login, BFF връща 401 JSON и SPA-то редиректва).
- [ ] Логове в JSON на stdout; никакви credentials в логовете.
- [ ] Тестове (vitest + supertest): 401 без cookie, login ok/fail, rate limit, CSRF отказ.

## Критерии за приемане
- [ ] `npm --prefix packages/hub-bff run test` зелен; `npm run typecheck` зелен.
