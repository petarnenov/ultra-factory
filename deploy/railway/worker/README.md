# Образ на AO worker (Railway)

Контейнерен образ, който пуска AO daemon-а като отдалечен worker хост. Съдържа
binary-то `ao` (чист Go, `CGO_ENABLED=0`), всичко, което `systemcheck` маркира
като Required (`git`, `tmux`), Node 24 с patch-натия Claude ACP адаптер на AO за
Claude Code Chat, `gh`, CLI-ите на агентите, както и `tini` и `jq` за
entrypoint-а.

## Билд

Контекстът на билда е коренът на репото. `Dockerfile.dockerignore` ограничава
контекста до файловете, които образът използва.

```bash
docker build -f deploy/railway/worker/Dockerfile -t ao-worker:dev .
docker run --rm ao-worker:dev sh -c 'ao version && git --version && tmux -V && node -v && claude --version && gh --version'
```

Multi-arch с buildx. Go binary-то и ACP адаптерът се билдват на хоста на билда;
само последният етап работи под емулация за другата архитектура:

```bash
docker buildx build --platform linux/amd64,linux/arm64 \
  -f deploy/railway/worker/Dockerfile -t ao-worker:dev .
```

Buildx builder от тип `docker-container` изпълнява arm64 стъпките с QEMU, който
идва с BuildKit. На builder без емулация я регистрирай веднъж на хоста с
`docker run --privileged --rm tonistiigi/binfmt --install arm64`.

Аргументи на билда:

| Аргумент | По подразбиране | Предназначение |
|---|---|---|
| `AGENT_NPM_PACKAGES` | `@anthropic-ai/claude-code@2.1.228 @openai/codex@0.147.0 opencode-ai@1.19.3` | Глобални npm инсталации с фиксирани версии. Дръж Claude Code в списъка. |
| `CURSOR_AGENT_VERSION` | `2026.08.11-e8db854` | Издание на Cursor Agent. Задай го празно, за да остане Cursor извън образа. |
| `AO_VERSION`, `AO_COMMIT` | `dev`, `unknown` | Записват се в `ao version`. |
| `GO_IMAGE`, `NODE_IMAGE` | `golang:1.27.1-bookworm`, `node:24-bookworm-slim` | Базови образи. |

`gh` идва от официалното apt хранилище на GitHub. То сервира само последното
издание, така че версията на `gh` следва датата на билда.

## Подредба и среда

| Път или променлива | Стойност |
|---|---|
| `/opt/ao/daemon/ao` | daemon и CLI, в `PATH` |
| `/opt/ao/acp-runtime` | patch-натият `claude-agent-acp`; `node/bin/node` сочи към Node на образа |
| `/usr/local/bin/ao-worker-entrypoint.sh` | entrypoint, пускан от `tini -s` |
| потребител | `ao`, uid/gid 10001 |
| `HOME` | `/data/home` |
| `WORKDIR` | `/data` |
| `AO_DATA_DIR` | `/data/ao` |
| `AO_RUN_FILE` | `/data/ao/running.json` |
| `AO_HARNESS_UPDATE_CHECKS` | `off` |
| `EXPOSE` | `3011` (remote-host listener) |
| `HEALTHCHECK` | `GET http://127.0.0.1:3001/healthz` вътре в контейнера |

Daemon-ът намира `acp-runtime/` до `daemon/`, същата подредба като при
`make daemon-dist`, така че `AO_ACP_RUNTIME_DIR` не е нужна. `tmux` идва от
Debian и се намира в `PATH`, така че и `AO_TMUX_BINARY` не е нужна.

## Размер

Измерено на 2026-10-16 от `main` @ f421a7098 със стойностите по подразбиране
по-горе:

| Платформа | Компресиран (слоеве в registry) | Разпакетиран |
|---|---|---|
| linux/amd64 | 559 MiB | 2.2 GB |
| linux/arm64 | 490 MiB | не е измерен |

Целта е под 1.5 GB компресиран. По-голямата част са CLI-ите на агентите: npm
слоят е около 1 GB разпакетиран (само Claude Code е около 300 MB).

## Entrypoint

`tini` е PID 1. Той препраща `SIGTERM` към daemon-а и прибира осиротелите
дъщерни процеси, които daemon като PID 1 оставя като зомбита. `-s` запазва това
работещо, когато runtime-ът добавя собствен init (`docker run --init`).
Entrypoint-ът (`ao-worker-entrypoint.sh`) се изпълнява при всеки старт и след
това се заменя с подадената команда или, ако няма такава, с `ao daemon` на
преден план:

1. **Като root** предава `/data`, `AO_DATA_DIR` и `HOME` на uid 10001
   (`chown -R`, само когато собственикът на най-горното ниво е грешен) и се
   пуска отново като uid 10001 със `setpriv`. Daemon-ът никога не работи като
   root.
2. Създава `AO_DATA_DIR` и `HOME` (mode 0700). Ако не са записваеми, излиза с
   подсказка за `RAILWAY_RUN_UID=0`.
3. **Само при първия старт** (няма `<AO_DATA_DIR>/mobile/config.json`): записва
   `{"enabled":true,"password":...,"lastPort":...,"noPublicTunnel":true}`
   с mode 0600. Daemon-ът възстановява remote-host listener-а от него при
   стартиране, така че не е нужен интерактивен `ao remote-host enable`.
   `noPublicTunnel` е това, което записва `ao remote-host enable`. Паролата е
   `AO_REMOTE_PASSWORD` или генерирана парола от 8 знака, която се логва само
   при този старт. Следващите стартове оставят файла както е и логват паролата
   маскирана; `ao remote-host status` в контейнера я показва.
4. Активира или отнема акаунт токена (виж по-долу).
5. Задава git идентичността и credential helper-а на `gh` от средата.
6. Изпълнява командата с `exec`. За `ao daemon` отделен помощен процес изчаква
   `/readyz` и логва `hostId` и адреса и порта на listener-а.

`AO_ENTRYPOINT_DRY_RUN=1` изпълнява стъпки 2-5 и печата командата, вместо да я
изпълни. `entrypoint_test.sh` го използва, за да тества логиката за
предварителното записване:

```bash
deploy/railway/worker/entrypoint_test.sh
```

### Променливи

| Променлива | За какво се използва |
|---|---|
| `AO_REMOTE_PASSWORD` | Паролата на remote-host при първия старт. Игнорира се, щом `config.json` съществува; изтрий файла, за да се запише наново. |
| `AO_REMOTE_PORT` | `lastPort` при първия старт, по подразбиране `3011`. Ако портът е зает, daemon-ът пада обратно към ефимерен порт; логът при старт показва истинския. |
| `AO_REMOTE_ACCOUNT_TOKEN` | 256-битов Bearer credential за публичен URL, например `openssl rand -hex 32`. |
| `AO_GIT_USER_NAME`, `AO_GIT_USER_EMAIL` | `git config --global user.name/user.email` в `HOME`. |
| `GH_TOKEN` (или `GITHUB_TOKEN`) | Чете се директно от `gh`; entrypoint-ът пуска и `gh auth setup-git`, за да го използва `git` през HTTPS. |
| `CLAUDE_CODE_OAUTH_TOKEN`, `ANTHROPIC_API_KEY`, `ANTHROPIC_AUTH_TOKEN` | Четат се директно от Claude Code, нищо не се записва. |
| `AO_PORT` | Портът на loopback API-то, който помощният процес при старт проверява, по подразбиране `3001`. |

Логът при старт посочва кои променливи с credential-и са зададени, но никога
стойностите им.

**Акаунт токен.** При всеки старт entrypoint-ът записва `accountTokenHash`,
SHA-256 hex на `AO_REMOTE_ACCOUNT_TOKEN`, в `config.json`. Listener-ът приема
този токен като Bearer credential наред с паролата.
`POST /api/v1/remote-host/account-token` на daemon-а не се използва: той създава
собствен случаен токен и не може да приеме такъв от извикващия. Издадени от
daemon-а `accountToken`/`accountTokenOwner` отпадат, когато променливата ги
замени. Когато променливата бъде премахната, следващият старт премахва хеш,
записан от entrypoint-а (хеш без `accountToken` до него); токен, издаден от
daemon-а, се запазва. Смяната на паролата в AO също отнема токена до следващия
старт, който го записва отново.

### Railway

- Монтирай volume-а на `/data`. Railway монтира volume-ите със собственик root, а
  собствеността, зададена по време на билда, не се пренася
  ([документация](https://docs.railway.com/volumes)). Задай `RAILWAY_RUN_UID=0`
  на услугата, за да стартира entrypoint-ът като root, да смени собственика на
  volume-а с chown и да премине към uid 10001. Без него контейнерът излиза с
  грешка за `/data/ao`.
- Остави командата за старт на услугата празна. По подразбиране тя е
  `ENTRYPOINT` на образа, който пуска `tini` и предварителното записване.
  Документацията на Railway не казва дали потребителска команда за старт запазва
  `ENTRYPOINT`
  ([документация](https://docs.railway.com/reference/build-and-start-commands)).

Локално именуван volume (`-v vol:/data`) започва с `/data` от образа, който
вече е собственост на uid 10001, така че там `RAILWAY_RUN_UID` не е нужна. За да
имитираш Railway, пусни с `-u 0`.
