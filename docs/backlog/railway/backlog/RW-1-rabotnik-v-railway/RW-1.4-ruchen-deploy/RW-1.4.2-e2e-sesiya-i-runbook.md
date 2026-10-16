# RW-1.4.2 · Таск: End-to-end сесия от desktop приложението и runbook

**Тип:** операции + документация. **Оценка:** 0.5 чд. **Стори:** RW-1.4.

## Контекст
- Desktop: Settings → Remote hosts (`frontend/src/renderer/components/settings/RemoteHostsSettings.tsx`), `~/.ao/remotes.json`.
- CLI: `ao host ls`, `ao spawn --host`.
- Credentials идват от env на работника, не от лаптопа (`docs/self-hosted-remote.md`).

## Събтаскове
- [ ] Добави хоста в desktop приложението с URL и парола; провери, че се появява `host.$hostId` маршрутът.
- [ ] Клонирай проект на работника (`/api/v1/projects/clone` през UI), пусни сесия с Claude Code, отвори терминал, прати съобщение, направи промяна, създай PR през `gh`.
- [ ] Провери `*.localhost` preview и artifacts през remote proxy-то; запиши какво не работи.
- [ ] Повтори ключовите стъпки с CLI: `ao spawn --host worker-1 ...`, `ao session ls --host worker-1`.
- [ ] Runbook `deploy/railway/README.md`: предпоставки, env променливи, добавяне в desktop, ротация на парола, redeploy, какво се губи при рестарт, troubleshooting (логове, `ao remote-host status` през `railway ssh`).

## Критерии за приемане
- [ ] Друг човек може да повтори процедурата по runbook-а без помощ.
- [ ] Списък „не работи/частично“ е записан и прехвърлен като бележки в RW-2 и RW-4.
