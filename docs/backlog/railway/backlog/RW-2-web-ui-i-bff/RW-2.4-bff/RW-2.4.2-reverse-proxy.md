# RW-2.4.2 · Таск: Reverse proxy за REST, SSE и `/mux`

**Тип:** Node TS. **Оценка:** 2–3 чд. **Стори:** RW-2.4. **Зависимости:** RW-2.4.1, RW-1.1.2.

## Контекст
- Според ADR B2: hub daemon-ът е просто хост `hub` в registry-то с URL `http://127.0.0.1:3011` и парола от `mobile/config.json` на същия volume. При B1: URL `http://127.0.0.1:3001` без Bearer. Кодът на proxy-то е еднакъв, различава се само записът.
- Daemon CORS (`backend/internal/httpd/cors.go:28-82`): заявки без `Origin` минават; с не-разрешен `Origin` → 403. BFF-ът **трябва да маха `Origin` header-а** (или да го сетва на `app://renderer`) при препращане, иначе browser Origin-ът ще бъде отхвърлен. Прецедент: `remote-proxy.ts:160-167`.
- `/mux` е WebSocket (`terminal_mux.go`), SSE е дълготраен response: изключи буферирането и timeouts за тези пътища.
- Blocked LAN маршрути (`lan_listener.go:67-78`) ще връщат 404 през :3011; SPA-то трябва да ги третира като „not available“ (RW-2.2).

## Събтаскове
- [ ] Маршрути: `/api/*`, `/mux`, `/healthz`→(BFF own), `/readyz` → хост `hub`; `/hosts/:hostId/api/*`, `/hosts/:hostId/mux`, `/hosts/:hostId/readyz` → хост по id от registry.
- [ ] За всяка препратена заявка: добави `Authorization: Bearer <password|accountToken>`, `X-AO-Expected-Host-ID: <hostId>`, премахни `Cookie` и `Origin`, запази `Accept`, `Content-Type`, `Last-Event-ID`.
- [ ] SSE: `Content-Type: text/event-stream`, flush на всеки chunk, `X-Accel-Buffering: no`, без idle timeout; при затваряне от клиента прекъсни upstream.
- [ ] WebSocket upgrade за `/mux` и `/hosts/:id/mux` с Bearer към upstream; двупосочен pipe; затваряне с код при грешка.
- [ ] Preview/artifact пътища (`/preview/files/` ползва `ao_conn` cookie upstream): препрати и този случай, като BFF-ът поставя cookie-то за upstream, не браузърът.
- [ ] Грешки: upstream недостъпен → 502 JSON с `requestId`; 401 от upstream → 502 „host credential rejected“ (не изливай 401 към браузъра, за да не го логне out).
- [ ] Тестове: fake upstream (http server) за REST, SSE поток с 3 събития, WS echo; проверка, че `Origin` и `Cookie` не стигат upstream, а `Authorization` стига.

## Критерии за приемане
- [ ] Renderer-ът в web build работи напълно през BFF-а срещу локален `ao daemon` с включен remote-host (`npm run dev:web:real` с `AO_DEV_API_TARGET=http://127.0.0.1:<bff>`).
