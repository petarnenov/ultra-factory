# RW-1.3.3 · Таск: Identity probe и Expected-Host-ID guard през proxy

**Тип:** backend Go + проверка. **Оценка:** 0.5 чд. **Стори:** RW-1.3.

## Цел
Потвърди, че `GET /api/v1/identity` (единственият неавтентикиран маршрут) и `X-AO-Expected-Host-ID` (421 при несъответствие, `lan_listener.go:138`) работят коректно през HTTPS proxy и че desktop/CLI клиентите ги ползват при добавяне на хост.

## Контекст
- `backend/internal/httpd/auth.go:245-252` identity exemption; ADR `0003`.
- `frontend/src/main/remote-request.ts:45-80` probe; `backend/internal/cli/remote_target.go:48-51` `remoteIdentity`.

## Събтаскове
- [ ] Провери, че identity отговорът не включва нищо чувствително, когда се сервира на публичен URL (само `hostId`, `apiVersion`).
- [ ] Провери, че `expectedHostGuard` не зависи от `Host` header-а (който proxy-то може да презапише).
- [ ] Ако `https://` URL към хост се отхвърля някъде в клиента (например scheme allowlist), поправи го. Търси `http://` допускания в `remote-request.ts`, `remote_target.go`.
- [ ] Добави тест в `remote_target_test.go` за `https` URL.

## Критерии за приемане
- [ ] `ao host ls` след добавяне на `https://worker.up.railway.app` показва hostId, `ao project ls --host worker` работи.
