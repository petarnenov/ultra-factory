# RW-1 · Епик: Работник в Railway, управляван от desktop приложението

**Цел.** Един `ao daemon` върви като Railway service с persistent volume, с включен автентикиран remote-host listener. Desktop приложението го добавя в Settings → Remote hosts и пуска сесия на него. Това валидира Railway networking, image-а и агентската автентикация преди да се инвестира в уеб UI.

**Резултат.** Публикуван worker image, entrypoint, ADR-и за решенията, и документ „как се деплойва работник ръчно“.

**Оценка:** 6–9 чд. **Етап:** MVP-1.

| Стори | Име | чд |
|---|---|---|
| RW-1.1 | Архитектурни решения (ADR) | 1–2 |
| RW-1.2 | Worker image и entrypoint | 2–3 |
| RW-1.3 | Remote-host listener зад reverse proxy | 2–3 |
| RW-1.4 | Ръчен деплой и връзка от desktop приложението | 1 |

**Критерии за приемане на епика**
- [ ] От desktop приложението се вижда работник в Railway, стартира се сесия с Claude Code, терминалът работи, PR се създава.
- [ ] След `railway redeploy` DB-то, worktrees и агентските credentials са запазени.
- [ ] Основният API (:3001) не е достъпен отвън. Единствената публична повърхност е :3011 с Bearer.

**Рискове.** IPv6-only private networking; размер на image-а; OAuth login на агентите в контейнер.
