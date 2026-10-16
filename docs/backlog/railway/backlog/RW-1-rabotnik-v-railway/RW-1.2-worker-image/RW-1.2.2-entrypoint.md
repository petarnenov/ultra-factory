# RW-1.2.2 · Таск: Entrypoint на работника

**Тип:** инфраструктура + shell. **Оценка:** 1 чд. **Стори:** RW-1.2.

## Цел
Скрипт, който при всеки старт подготвя volume-а, включва remote-host listener-а без интерактивна команда и пуска `ao daemon` на преден план.

## Контекст
- `ao remote-host enable` няма `--port` флаг (`backend/internal/cli/remote_host.go:116-117`). Портът идва само от `lastPort` в `<AO_DATA_DIR>/mobile/config.json`.
- `RestoreOnBoot` (`backend/internal/httpd/controllers/mobile.go:711-748`) чете `State{Enabled, Password, LastPort, LoopbackOnly, AccountTokenHash,...}` (`backend/internal/mobilebridge/config.go`) и стартира listener-а при boot.
- Паролата е 8 alnum символа, plaintext 0600. За публичен URL е по-добре 256-bit account token (`POST /api/v1/remote-host/account-token`, `router.go:213-227`).
- Daemon-ът отказва да стартира ако `running.json` сочи жив daemon (`daemon.go:211-224`); след crash файлът е stale и се чисти от `runfile.CheckStale`.
- `ao daemon` е hidden foreground команда (`cli/root.go:349-359`).

## Събтаскове
- [ ] Създай `/data/ao`, `/data/home` ако липсват; `chmod 700`.
- [ ] Ако `AO_REMOTE_PASSWORD` е зададен и `mobile/config.json` липсва, запиши го: `{"enabled":true,"password":"$AO_REMOTE_PASSWORD","lastPort":${AO_REMOTE_PORT:-3011}}` с mode 0600. Ако липсва и env-то, генерирай парола и я логни еднократно (маскирана след първия старт).
- [ ] Ако `AO_REMOTE_ACCOUNT_TOKEN` е зададен: след старт на daemon-а извикай `POST 127.0.0.1:3001/api/v1/remote-host/account-token` (виж формата в `controllers/mobile.go` IssueAccountToken) или запиши `accountTokenHash` директно (sha256 hex) в config.json. Избери едно и го документирай.
- [ ] Credentials от env в `$HOME`: `GH_TOKEN` → `gh auth setup-git` (или остави `GH_TOKEN` като е, `gh` го чете); `git config --global user.name/email` от `AO_GIT_USER_NAME/EMAIL`; Claude: `ANTHROPIC_API_KEY` или `CLAUDE_CODE_OAUTH_TOKEN` минават директно (виж `adapters/agent/claudecode/auth.go:33-93`).
- [ ] Стартирай `exec ao daemon` (PID 1 или под `tini`/`--init`), за да получава SIGTERM и да спира чисто.
- [ ] Логвай при старт: hostId (`GET 127.0.0.1:3001/api/v1/identity` след `/readyz`), порт, дали listener-ът е dual-stack (RW-1.3.2).

## Критерии за приемане
- [ ] Контейнер без volume стартира и показва парола в лога. Контейнер с volume и съществуващ config.json не променя паролата и hostId.
- [ ] `curl -H "Authorization: Bearer $PW" :3011/api/v1/projects` → 200 `[]`.
- [ ] `docker stop` завършва < 10 s без оставени `ao pty-host` процеси (`ps`).

## Проверка
Ръчна по горните команди + shell тест `deploy/railway/worker/entrypoint_test.sh` с `bats` или plain bash, който проверява pre-seed логиката без да пуска daemon-а (`AO_ENTRYPOINT_DRY_RUN=1`).
