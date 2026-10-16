# RW-1.1.2 · Таск: ADR за BFF пред daemon оркестратора в hub-а

**Тип:** ADR. **Оценка:** 0.5–1 чд. **Стори:** RW-1.1.

## Цел
Решение как уеб UI-ят в Railway достига daemon-а на hub-а, без да нарушава „primary listener stays loopback and unauthenticated“.

## Контекст
- `AGENTS.md` hard rules: „Do not add any other network-facing bind.“
- Два варианта:
  - **B1:** BFF (Node, в същия контейнер) proxy-ва към `127.0.0.1:3001`. Daemon-ът не се пипа. Публичната повърхност е BFF-ът с собствен login. Loopback-gated маршрутите стават достъпни за логнат потребител.
  - **B2:** hub daemon-ът също включва remote-host listener-а и BFF-ът говори с него на :3011 с Bearer, като с всеки друг работник. Губят се блокираните маршрути (`lan_listener.go:67-78`): system install, codex accounts, side-chat launch, desktop, browser.
- `remote-proxy.ts` в Electron main е прецедент за „доверен процес на същата машина, който инжектира credential“.

## Събтаскове
- [ ] Опиши двата варианта с таблица на достъпните маршрути.
- [ ] Вземи решение. Препоръка: **B2 по подразбиране** (еднакъв път за hub и работници, нулева промяна в daemon-а), с изричен списък на изгубените функции; B1 само ако списъкът е неприемлив.
- [ ] Определи модела на login на BFF-а: един оператор с парола + session cookie (MVP) и OIDC като опция.
- [ ] Определи къде живее credential-ът за всеки хост: `hosts.json` на volume на hub-а, 0600, формат съвместим с `remotes.json` (`label`, `url`, `password`, `hostId`).
- [ ] Напиши `docs/adr/000N-web-bff-for-hub.md`.

## Критерии за приемане
- [ ] ADR-ът фиксира варианта, auth модела и списъка на функциите, които не са налични в уеб.
- [ ] RW-2.4 може да се имплементира без нови решения.
