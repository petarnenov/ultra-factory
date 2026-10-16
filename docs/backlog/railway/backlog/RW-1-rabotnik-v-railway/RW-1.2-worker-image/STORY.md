# RW-1.2 · Стори: Worker image и entrypoint

**Като** оператор **искам** публикуван контейнерен image, който при старт в Railway вдига `ao daemon` с включен remote-host listener и всичкото му състояние на volume, **за да** мога да създам работник само с image URL и env променливи.

**Оценка:** 2–3 чд. **Зависимости:** няма за кода; RW-1.3 за пълна работа зад proxy. **Блокира:** RW-1.4, RW-3.

| Таск | Име | чд |
|---|---|---|
| RW-1.2.1 | Dockerfile на работника | 1 |
| RW-1.2.2 | Entrypoint: volume, pre-seed на listener-а, credentials от env | 1 |
| RW-1.2.3 | CI: build и publish в GHCR | 0.5–1 |

**Критерии за приемане**
- [ ] `docker run -e AO_REMOTE_PASSWORD=... -v vol:/data ghcr.io/<org>/ao-worker:<tag>` дава `GET :3011/api/v1/identity` → 200 с `hostId`.
- [ ] `ao remote-host status` вътре в контейнера показва Enabled, адрес и парола.
- [ ] След рестарт hostId, парола, DB и worktrees са същите.
- [ ] Image-ът съдържа всичко, което `systemcheck` маркира като Required.
