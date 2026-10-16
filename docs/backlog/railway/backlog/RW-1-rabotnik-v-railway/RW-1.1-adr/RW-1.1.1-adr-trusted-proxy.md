# RW-1.1.1 · Таск: ADR за доверие на reverse proxy зад remote-host listener-а

**Тип:** ADR. **Оценка:** 0.5 чд. **Стори:** RW-1.1.

## Цел
Решение дали и как `authMiddleware` да ползва IP на клиента от proxy header, когато daemon-ът върви зад Railway edge (TLS terminating proxy).

## Контекст
- `backend/internal/httpd/auth.go:105` — lockout 5 грешки/мин на `sourceKey` = `RemoteAddr`.
- `auth.go:260-270` — `CF-Connecting-IP` се ползва само когда peer-ът е loopback (cloudflared).
- `X-Forwarded-For` не се чете никъде. Зад Railway всички клиенти изглеждат с един IP и един нападател блокира всички за минута.
- Прецедент: `docs/adr/0004-*` (Cloudflare tunnel).

## Събтаскове
- [ ] Проучи кой header Railway edge подава (`X-Forwarded-For`, `X-Real-IP`) и дали peer IP-то на proxy-то е предвидимо.
- [ ] Избери модел: (а) opt-in env `AO_REMOTE_TRUSTED_PROXY=1` → ползва първия `X-Forwarded-For`; (б) алтернатива: lockout per credential вместо per IP. Препоръка: (а) + запазен per-IP lockout, никога default on.
- [ ] Опиши защо header-ът не може да се spoof-ва (клиентът не достига socket-а директно; private networking е отделен случай).
- [ ] Напиши `docs/adr/000N-trusted-proxy-remote-host.md`: Context, Decision, Consequences, Security notes.
- [ ] Добави препратка в `AGENTS.md` hard rules.

## Критерии за приемане
- [ ] ADR-ът отговаря на: кога флагът е безопасен, кога е опасен (LAN режим без proxy), какво се логва.
- [ ] Ревюирано от втори човек.

## Проверка
Няма код. Ревю на документа.
