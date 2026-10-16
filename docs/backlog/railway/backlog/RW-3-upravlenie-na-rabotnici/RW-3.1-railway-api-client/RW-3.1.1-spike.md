# RW-3.1.1 · Таск: Spike на Railway public API

**Тип:** проучване. **Оценка:** 0.5 чд. **Стори:** RW-3.1.

## Събтаскове
- [ ] Прочети Railway public API документацията (GraphQL endpoint `https://backboard.railway.com/graphql/v2` към момента на писане; потвърди). Запиши: как се създава service от Docker image в даден environment, как се създава volume и се монтира, как се задават variables, как се задава target port/публичен домейн, как се чете статусът на deployment, как се трие service и volume.
- [ ] Провери кой тип токен е нужен (project token vs account/team token) и минималния scope за горните операции.
- [ ] Провери лимити: rate limit, брой services/volumes на project, максимален размер на volume.
- [ ] Направи операциите ръчно с `curl` срещу `ao-lab` project и запиши работещите заявки + отговори (с маскирани id-та) в `packages/hub-bff/docs/railway-api-notes.md`. Те стават fixtures за тестовете в RW-3.1.2.
- [ ] Запиши времето от create до online на service от worker image-а.

## Критерии за приемане
- [ ] Документът покрива всичките 7 операции с реално изпълнени заявки.
