# RW-1.3.1 · Таск: Trusted-proxy режим за lockout

**Тип:** backend Go, security. **Оценка:** 1 чд. **Стори:** RW-1.3. **Зависимости:** RW-1.1.1.

## Цел
При `AO_REMOTE_TRUSTED_PROXY=1` `authMiddleware` ползва клиентския IP от `X-Forwarded-For` (първия елемент) за ключа на lockout-а, вместо `RemoteAddr`.

## Контекст
- `backend/internal/httpd/auth.go:144` `sourceKey`; `auth.go:260-270` CF-Connecting-IP логика за loopback peer. Разшири същото място, не добавяй нов middleware.
- Config: `backend/internal/config/config.go` `Load()` с валидация на env (виж `AO_ALLOWED_ORIGINS` парсинг :282-299 за стила). Тестове в `config_test.go` изброяват всички `AO_*` ключове (:14), добави новия.
- Тестове за auth: `backend/internal/httpd/auth_test.go` (lockout табличните тестове).

## Събтаскове
- [ ] `config.go`: поле `RemoteTrustedProxy bool` от `AO_REMOTE_TRUSTED_PROXY` (приема `1/true/on`), default false. Добави в `config_test.go` env списъка.
- [ ] Подай флага до `authMiddleware` (през `LANManager` или `Server` options, където днес се подава CF логиката).
- [ ] В `authMiddleware`: ако флагът е включен и `X-Forwarded-For` има валиден IP, `src = първи IP`. Иначе fallback към текущото поведение. Не променяй логиката за `CF-Connecting-IP`.
- [ ] Логвай предупреждение веднъж при старт: „trusted proxy mode on; only enable behind a reverse proxy“.
- [ ] Таблични тестове: без флаг → RemoteAddr; с флаг + валиден XFF → XFF; с флаг + невалиден/липсващ XFF → RemoteAddr; с флаг два клиента зад един peer → независим lockout.
- [ ] Документирай в `docs/self-hosted-remote.md` секция „Behind a reverse proxy“.

## Критерии за приемане
- [ ] `go test ./internal/httpd/...` зелен, покритие на четирите случая.
- [ ] `make lint` чист.

## Проверка
`cd backend && go test ./internal/httpd/ -run 'Auth|Lockout' -v`
