# RW-3 · Епик: Добавяне и махане на работници от уеб UI

**Цел.** От Settings → Workers в уеб UI-я операторът натиска „Добави работник“, задава име, ключове и размер, и BFF-ът създава Railway service с volume от worker image-а, чака го да стане online и го регистрира. „Премахни“ изтрива service-а и volume-а след потвърждение.

**Оценка:** 5–8 чд. **Етап:** пълна цел. **Зависимости:** RW-1 (image, валидиран networking), RW-2.4 (BFF, registry).

| Стори | Име | чд |
|---|---|---|
| RW-3.1 | Railway API клиент в BFF-а | 1.5–2 |
| RW-3.2 | Lifecycle на работник: provisioning → online → removing | 2–3 |
| RW-3.3 | UI: Workers екран с добавяне и премахване | 1.5–3 |

**Критерии за приемане на епика**
- [ ] Добавяне на работник от UI отнема < 5 мин до статус online без ръчни стъпки в Railway.
- [ ] Премахването показва активните сесии, изисква потвърждение с името, трие service + volume, маха записа.
- [ ] Railway токенът не напуска BFF-а; лимит на брой работници е конфигурируем (`AO_HUB_MAX_WORKERS`, default 5).

**Отворени въпроси (решават се в RW-3.1.1).** Точните GraphQL мутации на Railway public API за service от image + volume + variables + domain; project-scoped vs account token; поведение при зает service name.
