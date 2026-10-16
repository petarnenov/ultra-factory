# RW-2.4.3 · Таск: Registry на хостове и API за добавяне/махане по адрес

**Тип:** Node TS. **Оценка:** 1–2 чд. **Стори:** RW-2.4. **Зависимости:** RW-2.4.1.

## Контекст
- Формат, съвместим с desktop `~/.ao/remotes.json` и CLI `remoteEntry` (`backend/internal/cli/remote_target.go:31-37`): `{label, url, password, hostId, accountUserId?}`. Разшири с `source: "manual"|"railway"`, `railway?: {serviceId, volumeId, environmentId}`, `createdAt`, `status`.
- Път: `AO_HUB_DATA_DIR/hosts.json` на volume-а на hub-а, 0600, атомарен запис (tmp + rename).
- Identity probe: `GET <url>/api/v1/identity` → `{hostId, apiVersion}` (неавтентикиран); после `GET <url>/api/v1/projects` с Bearer за валидиране на паролата.

## Събтаскове
- [ ] `hosts-store.ts`: load/save/list/get/add/remove с валидация (`url` само `http(s)://`, label уникален).
- [ ] BFF API (auth required): `GET /hub/hosts` (без пароли), `POST /hub/hosts` `{label,url,password}` → probe identity + auth check → запис; `DELETE /hub/hosts/:id` (не трие нищо в Railway); `POST /hub/hosts/:id/probe` → статус.
- [ ] Статус polling: на 30 s probe на всеки хост, `status: online|unreachable|auth_failed`, изложен в `GET /hub/hosts`.
- [ ] Записът `hub` се създава автоматично при старт от `mobile/config.json` на същия volume (B2) и не може да се трие.
- [ ] Тестове със fake хост.

## Критерии за приемане
- [ ] Добавяне на валидиран работник от RW-1.4 през `curl` към BFF-а, после `/hosts/<id>/api/v1/projects` работи.
