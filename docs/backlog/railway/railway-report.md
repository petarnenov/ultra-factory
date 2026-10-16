# AO в Railway: оркестратор с уеб UI и управление на работници

**Дата:** 2026-10-16. **База:** `main` @ eb03ffaf6.

## Цел
Оркестраторът (AO daemon) и неговият UI да вървят в Railway и да се ползват от браузър, без desktop приложение. От този UI потребителят добавя и маха работници: машини в Railway, на които се изпълняват агентските сесии. Отчетът оценява какво има в кода днес, кой подход е подходящ, каква е сложността и колко време ще отнеме.

## 1. Изводи
- **Не е само деплой.** Изисква нов код, а два от компонентите заобикалят hard rules в `AGENTS.md`. За тях е нужен ADR.
- **Работниците почти ги има.** Режимът „self-hosted remote host“ е `ao daemon` плюс `ao remote-host enable`. Той дава автентикиран listener на :3011. `make daemon-dist` е правен точно за „remote worker host“. CLI-ят вече може `ao spawn --host <worker>`.
- **Липсват три неща:**
  - уеб версия на UI-я (днес е само Electron);
  - сървър (BFF) пред нея за login, proxy и Bearer;
  - модул, който създава и трие Railway services през Railway API.
- **Препоръка:** вариант A (hub + daemon работници), на фази. MVP с работещ работник се вижда от desktop приложението след 5–8 дни. Пълната цел е **~25–40 човекодни (5–8 седмици за един разработчик)**.

## 2. Какво има в кода днес
| Компонент | Състояние | Къде |
|---|---|---|
| Основен API на daemon-а | Само `127.0.0.1:3001`, без auth, bind host-ът не се конфигурира | `backend/internal/config/config.go:20-26` |
| Remote-host listener | `0.0.0.0:3011`, Bearer парола или 256-bit account token. Блокира част от маршрутите | `backend/internal/httpd/lan_listener.go`, `auth.go` |
| Lockout | 5 грешни опита/мин на `RemoteAddr`; `X-Forwarded-For` се игнорира | `backend/internal/httpd/auth.go:105,144` |
| Работник без клониране на репото | `make daemon-dist` (Node 24 + ACP, без tmux) и `setup-self-hosted.sh --install-only` за контейнери | `scripts/build-daemon-dist.sh`, `docs/self-hosted-remote.md` |
| Delegation към хостове | `ao spawn/send/session --host`, `ao host ls` | `backend/internal/cli/remote_target.go` |
| Multi-host в UI | маршрути `host.$hostId.*`, `clientForHost`, Settings → Remote hosts | `frontend/src/renderer/lib/host-clients.ts` |
| Proxy с инжектиран Bearer | токен в пътя, защото SSE/WS не пращат хедъри (Node код в Electron main) | `frontend/src/main/remote-proxy.ts` |
| Renderer в браузър | фасада `aoBridge` с готов browser stub. `dev:web` е само с mock данни | `frontend/src/renderer/lib/bridge.ts`, `preview-mode.ts` |
| Base URL | `VITE_AO_API_BASE_URL` се поддържа, но `applyDaemonStatus` натрапва `http://127.0.0.1:port` | `lib/api-client.ts:12-26`, `lib/daemon-status.ts:8-11` |
| CSP | `connect-src` е заковано на `127.0.0.1` | `frontend/vite.renderer.csp.ts:80-86` |
| AO Cloud control plane | Go + Postgres(RLS) + WorkOS. Пуска по един sandbox на сесия; providers са docker/coder/freestyle/createos, няма Railway. Деплойва се на AWS ECS. Hosted web UI-ят е в private submodule, който го няма тук | `cloud/`, `cloud/internal/sandbox/provider.go:181` |
| Railway/Fly/k8s конфигурация | няма | — |

## 3. Варианти

### A. Hub + daemon работници (препоръчан)
```
Браузър ──HTTPS──▶ [Railway: hub]
                    ├─ BFF (Node): login, статичен SPA, proxy /api /mux SSE, Railway API клиент
                    └─ ao daemon (оркестратор, 127.0.0.1:3001) + volume
                         │  private network (*.railway.internal:3011, Bearer)
                         ├─▶ [Railway: worker-1] ao daemon + remote-host listener + volume
                         └─▶ [Railway: worker-N] ...
```
- **Работник:** Railway service от общ image с git, tmux, node24, gh и агентските CLI-та. Има собствен volume за `HOME`, `AO_DATA_DIR` и worktrees. Агентските ключове идват като env (`ANTHROPIC_API_KEY`/`CLAUDE_CODE_OAUTH_TOKEN`, `GH_TOKEN`, …).
- **Добавяне:** BFF вика Railway GraphQL: service от image, volume, variables, deploy. После чака `GET /api/v1/identity` да отговори и записва хоста в registry-то.
- **Махане:** предупреждение за активните сесии, после изтриване на service-а и volume-а и махане от registry-то.
- **Оркестраторът в hub** делегира към работниците с `ao spawn --host`. Това вече е имплементирано.

### B. AO Cloud control plane в Railway + нов Railway sandbox provider
- Работниците са автоматични, по един на сесия. Не се добавят ръчно. Това се разминава с искането.
- `docker` provider-ът не е възможен: няма Docker socket, а и hosted режимът го отхвърля.
- Hosted режимът изисква WorkOS и Postgres роля без superuser/BYPASSRLS.
- **Няма UI** в репото. Ще трябва нов или desktop приложението с `AO_CLOUD_CONTROL_PLANE_URL`.
- **Оценка:** ~35–60 дни за control plane + provider, плюс ~20–40 дни за уеб UI. Не го препоръчвам за тази цел.

### C. Само един daemon в Railway, достъп от desktop приложението
Това е фаза 1 на A, ~3–5 дни. Няма уеб UI и няма управление на работници.

## 4. План за вариант A: сложност и време
Оценките са в човекодни за разработчик, който познава репото.

| # | Работа | Сложност | Дни |
|---|---|---|---|
| 0 | **ADR-и:** (а) BFF излага loopback API-то на hub-а зад собствен auth; (б) доверие на `X-Forwarded-For` зад Railway proxy | средна (решение, не код) | 1–2 |
| 1 | **Image на работника:** Dockerfile на базата на `daemon-dist` + tmux + агентски CLI-та. Entrypoint: `HOME`/`AO_DATA_DIR`/`AO_RUN_FILE` на volume, pre-seed на `mobile/config.json` (enabled, парола, порт 3011), `AO_HARNESS_UPDATE_CHECKS=off`, healthcheck. Публикуване в GHCR | ниска–средна | 2–3 |
| 2 | **Listener зад proxy:** (а) lockout: днес 5 грешни опита блокират всички клиенти за минута, нужен е trusted-proxy режим; (б) dual-stack bind (`[::]`) за IPv6 private networking | средна (security код) | 2–3 |
| 3 | **Ръчен деплой на 1 работник и връзка от desktop-а** през Settings → Remote hosts. Проверка на private networking (IPv4/IPv6 за `0.0.0.0` bind), сесия, терминал, PR | ниска | 1 |
| 4 | **Web build на renderer-а:** web `daemon` адаптер (health → ready), base URL без `127.0.0.1`, CSP, скриване на Electron-only функциите (browser panel, editor handoff, CLI link, auto-update), клавишни комбинации в renderer-а, Web Notifications, localStorage за ui settings | средна | 5–8 |
| 5 | **BFF (Node):** login (един потребител или OIDC) със session cookie, статичен SPA, reverse proxy за REST/SSE/`/mux` към hub daemon-а и към всеки работник с Bearer (порт на `remote-proxy.ts`), registry на хостовете върху volume | средна–висока | 5–8 |
| 6 | **Hub image:** daemon + BFF + SPA в един контейнер с два процеса под supervisor | ниска–средна | 1–2 |
| 7 | **Управление на работници:** Railway GraphQL клиент (create/volume/vars/deploy/status/delete), async статуси (provisioning → ready → failed), UI диалог „Добави работник“ (име, ключове, размер) и „Премахни“ с потвърждение | средна–висока | 5–8 |
| 8 | **Hardening:** тайни, лимит на брой работници (разходи), логове, backup на volumes, e2e тест (Playwright web), документация | средна | 3–5 |
| | **Общо** | | **25–40** |

Беклогът по тази таблица (епик → стори → таск → събтаск) е в директорията `backlog/` до този файл.

**Етапи:**
- **MVP-1** (фази 0–3, ~5–8 дни): работници в Railway, управлявани от desktop приложението.
- **MVP-2** (+4–6, още ~11–18 дни): уеб UI в Railway с ръчно добавяне на работници по адрес.
- **Пълна цел** (+7–8, още ~8–13 дни): добавяне и махане на работници с един клик.

## 5. Рискове и отворени въпроси
1. **Hard rules в `AGENTS.md`.** „Do not add any other network-facing bind“ и основният listener е само loopback. BFF, който публикува loopback API-то, е архитектурно решение и трябва ADR. Компромисът е hub-ът да минава през :3011 като другите работници. Тогава се губят маршрутите, които listener-ът блокира: system install, codex accounts, side-chat launch, preview server.
2. **Сигурност.** Достъпът до UI-я е равен на shell на всички работници. Railway API токенът в hub-а може да създава платени ресурси. Нужни са силен login, лимити и project-scoped токен.
3. **Рестарт и redeploy убиват живите агентски процеси.** PTY host и tmux не оцеляват при рестарт на контейнера. Остават само DB и worktrees на volume-а. Ъпдейтът на image-а трябва да се прави, когато работникът е празен.
4. **Агентска автентикация.** Интерактивният OAuth login в контейнер е неудобен, затова ключовете вървят през env vars. Ако се ползва абонамент вместо API ключ, условията на доставчика трябва да се проверят.
5. **Railway специфики, за проверка:**
   - **IPv6.** Private networking в Railway е (поне исторически) само IPv6, а listener-ът на работника се bind-ва на `0.0.0.0`, което е само IPv4 (`backend/internal/httpd/lan_listener.go:202`). Ако това е така и днес, hub-ът няма да стигне до работниците по `*.railway.internal` без dual-stack bind (`[::]`). Това е първото нещо за проверка във фаза 3;
   - `ao remote-host enable` няма `--port` флаг. Портът идва само от `lastPort` в `<AO_DATA_DIR>/mobile/config.json`, който `RestoreOnBoot` чете при старт (`controllers/mobile.go:711`). Entrypoint-ът на работника трябва да pre-seed-ва този файл;
   - един volume на service, без zero-downtime deploy при volume;
   - схемата и лимитите на Railway public GraphQL API;
   - размерът на image-а с агентските CLI-та.
6. **Функции, които губиш в браузъра:** вграденият browser panel, агентският CDP браузър, анотациите, импортът на профили, editor handoff и `*.localhost` previews.
7. **Един daemon на работник, един потребител.** Няма multi-tenancy. За екип е нужен вариант B.

## 6. Препоръчана следваща стъпка
Фаза 0 и фаза 1 + 3: ADR и един ръчно деплойнат работник, вързан към desktop приложението. Струва ~1 седмица. Валидира Railway networking, image-а и агентските ключове, преди да се инвестира в уеб UI и BFF.
