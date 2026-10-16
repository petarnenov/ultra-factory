# RW-4.2.2 · Таск: Лимити и наблюдение на разходите

**Тип:** операции + Node TS. **Оценка:** 0.5 чд. **Стори:** RW-4.2.

## Събтаскове
- [ ] `AO_HUB_MAX_WORKERS` вече е в RW-3.2.2; добави `AO_HUB_MAX_VOLUME_GB` за spec валидация.
- [ ] Idle работник: ако 0 сесии > N часа (`AO_HUB_IDLE_STOP_HOURS`, default off) → UI предупреждение; автоматично спиране само ако provider-ът поддържа stop/start без загуба на volume (провери в RW-3.1.1).
- [ ] Runbook: как се чете Railway usage за project-а; препоръчителни размери.
