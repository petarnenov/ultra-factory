# RW-1.2.1 · Таск: Dockerfile на работника

**Тип:** инфраструктура. **Оценка:** 1 чд. **Стори:** RW-1.2.

## Цел
`deploy/railway/worker/Dockerfile` (нова директория), който билдва `ao` (pure Go, CGO off) и инсталира runtime зависимостите.

## Контекст
- Прецеденти: `test/cli/Dockerfile` (multi-stage Go build, `golang:1.27.1-bookworm`), `cloud/Dockerfile` worker target (`node:22-trixie-slim`, claude-code, codex, opencode, cursor-agent, gh, git, tmux, user uid 10001).
- `scripts/build-daemon-dist.sh` бъндълва Node 24 и patched `claude-agent-acp` в `acp-runtime/`. Бъндълнатият Node иска glibc ≥ 2.28, Alpine не става.
- Required binaries по `backend/internal/service/systemcheck/systemcheck.go:181-197`: git, tmux. ACP chat изисква node.
- `AO_TMUX_BINARY` (`backend/internal/tmuxbin/resolve.go:36`) ако tmux не е бъндълнат.

## Събтаскове
- [ ] Stage 1: `go build -trimpath -o /out/ao ./cmd/ao` с `CGO_ENABLED=0` от `backend/`.
- [ ] Stage 2: база `node:24-bookworm-slim` (или trixie). `apt-get install git tmux bash ca-certificates openssh-client curl` + `gh` от официалния apt repo.
- [ ] Инсталирай агентски CLI-та като build ARG списък с пинати версии: `@anthropic-ai/claude-code`, `@openai/codex`, `opencode-ai`; по желание `cursor-agent`. Копирай patched `claude-agent-acp` от `frontend/scripts/build-acp-runtime.mjs` output или преизползвай логиката на `build-daemon-dist.sh`.
- [ ] Non-root user `ao` (uid 10001), `HOME=/data/home`, `WORKDIR /data`.
- [ ] `ENV AO_DATA_DIR=/data/ao AO_RUN_FILE=/data/ao/running.json AO_HARNESS_UPDATE_CHECKS=off`.
- [ ] `EXPOSE 3011`, `HEALTHCHECK` към `http://127.0.0.1:3001/healthz`.
- [ ] `ENTRYPOINT ["/usr/local/bin/ao-worker-entrypoint.sh"]` (RW-1.2.2).
- [ ] Multi-arch (`linux/amd64`, `linux/arm64`) чрез buildx.

## Критерии за приемане
- [ ] Image < 1.5 GB компресиран (измери и запиши в README на директорията).
- [ ] `docker run --rm <img> ao version` работи; `which git tmux node claude gh` връща пътища.
- [ ] Няма root процеси след старт.

## Проверка
```
docker build -f deploy/railway/worker/Dockerfile -t ao-worker:dev .
docker run --rm ao-worker:dev sh -c 'ao version && git --version && tmux -V && node -v && claude --version && gh --version'
```
