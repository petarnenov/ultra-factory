# RW-1.2.1 · Task: Worker Dockerfile
**Status:** ✅ done (#61)

**Type:** infrastructure. **Estimate:** 1 pd. **Story:** RW-1.2.

## Goal
`deploy/railway/worker/Dockerfile` (new directory) that builds `ao` (pure Go, CGO off) and installs the runtime dependencies.

## Context
- Precedents: `test/cli/Dockerfile` (multi-stage Go build, `golang:1.27.1-bookworm`), the `cloud/Dockerfile` worker target (`node:22-trixie-slim`, claude-code, codex, opencode, cursor-agent, gh, git, tmux, user uid 10001).
- `scripts/build-daemon-dist.sh` bundles Node 24 and a patched `claude-agent-acp` into `acp-runtime/`. The bundled Node requires glibc ≥ 2.28, so Alpine will not work.
- Required binaries per `backend/internal/service/systemcheck/systemcheck.go:181-197`: git, tmux. ACP chat requires node.
- `AO_TMUX_BINARY` (`backend/internal/tmuxbin/resolve.go:36`) if tmux is not bundled.

## Subtasks
- [ ] Stage 1: `go build -trimpath -o /out/ao ./cmd/ao` with `CGO_ENABLED=0` from `backend/`.
- [ ] Stage 2: base `node:24-bookworm-slim` (or trixie). `apt-get install git tmux bash ca-certificates openssh-client curl` + `gh` from the official apt repo.
- [ ] Install the agent CLIs as a build ARG list with pinned versions: `@anthropic-ai/claude-code`, `@openai/codex`, `opencode-ai`; optionally `cursor-agent`. Copy the patched `claude-agent-acp` from the `frontend/scripts/build-acp-runtime.mjs` output or reuse the logic of `build-daemon-dist.sh`.
- [ ] Non-root user `ao` (uid 10001), `HOME=/data/home`, `WORKDIR /data`.
- [ ] `ENV AO_DATA_DIR=/data/ao AO_RUN_FILE=/data/ao/running.json AO_HARNESS_UPDATE_CHECKS=off`.
- [ ] `EXPOSE 3011`, `HEALTHCHECK` against `http://127.0.0.1:3001/healthz`.
- [ ] `ENTRYPOINT ["/usr/local/bin/ao-worker-entrypoint.sh"]` (RW-1.2.2).
- [ ] Multi-arch (`linux/amd64`, `linux/arm64`) via buildx.

## Acceptance criteria
- [ ] Image < 1.5 GB compressed (measure it and record it in the directory's README).
- [ ] `docker run --rm <img> ao version` works; `which git tmux node claude gh` returns paths.
- [ ] No root processes after startup.

## Verification
```
docker build -f deploy/railway/worker/Dockerfile -t ao-worker:dev .
docker run --rm ao-worker:dev sh -c 'ao version && git --version && tmux -V && node -v && claude --version && gh --version'
```
