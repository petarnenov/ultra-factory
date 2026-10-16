# syntax=docker/dockerfile:1

# Образ на AO worker за Railway: daemon-ът `ao`, всичко, което systemcheck
# маркира като Required (git, tmux), Node за Claude Code Chat, gh, CLI-ите на
# агентите, както и tini и jq за entrypoint-а.
#
# Контекстът на билда е КОРЕНЪТ НА РЕПОТО:
#   docker build -f deploy/railway/worker/Dockerfile -t ao-worker:dev .
#   docker buildx build --platform linux/amd64,linux/arm64 \
#     -f deploy/railway/worker/Dockerfile -t ao-worker:dev .
#
# Подредбата повтаря `make daemon-dist` (scripts/build-daemon-dist.sh):
# /opt/ao/daemon/ao стои до /opt/ao/acp-runtime, така че daemon-ът намира
# Claude ACP адаптера без AO_ACP_RUNTIME_DIR.

ARG GO_IMAGE=golang:1.27.1-bookworm
# ACP адаптерът изисква Node 22+ върху glibc 2.28+; Alpine (musl) не работи.
ARG NODE_IMAGE=node:24-bookworm-slim

# ---- етап 1: binary-то ao, крос-компилирано на хоста на билда ----
FROM --platform=$BUILDPLATFORM ${GO_IMAGE} AS build
ARG TARGETOS=linux
ARG TARGETARCH
ARG AO_VERSION=dev
ARG AO_COMMIT=unknown
WORKDIR /src/backend

COPY backend/go.mod backend/go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download

COPY backend ./
# SQLite на чист Go (modernc) се билдва с изключен CGO -> статично binary.
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath \
      -ldflags "-s -w -X github.com/aoagents/agent-orchestrator/backend/internal/cli.Version=${AO_VERSION} -X github.com/aoagents/agent-orchestrator/backend/internal/cli.Commit=${AO_COMMIT}" \
      -o /out/ao ./cmd/ao

# ---- етап 2: patch-натият Claude ACP адаптер на AO ----
# Адаптерът е платформено независим JavaScript, затова се билдва веднъж на
# хоста на билда със същия скрипт и patch-ове като десктоп приложението и
# daemon-dist. Скриптът сваля и Node runtime за хоста на билда; тук той се
# изтрива, защото крайният образ вече съдържа Node.
FROM --platform=$BUILDPLATFORM ${NODE_IMAGE} AS acp
WORKDIR /src/frontend
COPY frontend/acp-runtime ./acp-runtime
COPY frontend/scripts/build-acp-runtime.mjs frontend/scripts/build-acp-runtime-helpers.mjs ./scripts/
RUN node scripts/build-acp-runtime.mjs && \
    test -f resources/acp-runtime/node_modules/@agentclientprotocol/claude-agent-acp/dist/index.js && \
    rm -rf resources/acp-runtime/node resources/acp-runtime/.ao-acp-runtime.json

# ---- етап 3: worker-ът ----
FROM ${NODE_IMAGE} AS worker
ARG TARGETARCH
# npm пакети, инсталирани глобално, всеки с фиксирана версия. Замени целия
# списък, за да добавиш или махнеш CLI; Claude Code трябва да остане, защото
# Claude Code Chat го пуска.
ARG AGENT_NPM_PACKAGES="@anthropic-ai/claude-code@2.1.228 @openai/codex@0.147.0 opencode-ai@1.19.3"
# По избор; билдни с --build-arg CURSOR_AGENT_VERSION=, за да го пропуснеш.
ARG CURSOR_AGENT_VERSION=2026.08.11-e8db854
SHELL ["/bin/bash", "-o", "pipefail", "-c"]

# gh идва от официалното apt хранилище на GitHub, което сервира само
# последното издание, затова версията му следва датата на билда, а не е
# фиксирана.
RUN apt-get update && \
    apt-get install --yes --no-install-recommends \
        bash \
        ca-certificates \
        curl \
        git \
        jq \
        openssh-client \
        procps \
        tini \
        tmux && \
    install -d -m 0755 /etc/apt/keyrings && \
    curl --fail --location --silent --show-error \
        https://cli.github.com/packages/githubcli-archive-keyring.gpg \
        -o /etc/apt/keyrings/githubcli-archive-keyring.gpg && \
    chmod go+r /etc/apt/keyrings/githubcli-archive-keyring.gpg && \
    echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/githubcli-archive-keyring.gpg] https://cli.github.com/packages stable main" \
        > /etc/apt/sources.list.d/github-cli.list && \
    apt-get update && \
    apt-get install --yes --no-install-recommends gh && \
    rm -rf /var/lib/apt/lists/* && \
    gh --version

RUN npm install --global ${AGENT_NPM_PACKAGES} && \
    npm cache clean --force && \
    claude --version

RUN if [ -n "${CURSOR_AGENT_VERSION}" ]; then \
        case "${TARGETARCH}" in \
            amd64) cursor_arch=x64 ;; \
            arm64) cursor_arch=arm64 ;; \
            *) echo "unsupported Cursor Agent architecture: ${TARGETARCH}" >&2; exit 1 ;; \
        esac && \
        mkdir -p "/opt/cursor-agent/${CURSOR_AGENT_VERSION}" && \
        curl --fail --location --silent --show-error \
            "https://downloads.cursor.com/lab/${CURSOR_AGENT_VERSION}/linux/${cursor_arch}/agent-cli-package.tar.gz" \
            | tar --strip-components=1 -xzf - -C "/opt/cursor-agent/${CURSOR_AGENT_VERSION}" && \
        ln -s "/opt/cursor-agent/${CURSOR_AGENT_VERSION}/cursor-agent" /usr/local/bin/cursor-agent && \
        cursor-agent --version; \
    fi

COPY --from=build /out/ao /opt/ao/daemon/ao
COPY --from=acp /src/frontend/resources/acp-runtime /opt/ao/acp-runtime
# Daemon-ът изисква acp-runtime/node/bin/node; насочи го към Node на образа.
RUN mkdir -p /opt/ao/acp-runtime/node/bin && \
    ln -s /usr/local/bin/node /opt/ao/acp-runtime/node/bin/node
COPY --chmod=0755 deploy/railway/worker/ao-worker-entrypoint.sh /usr/local/bin/ao-worker-entrypoint.sh

RUN groupadd --gid 10001 ao && \
    useradd --uid 10001 --gid ao --home-dir /data/home --no-create-home --shell /bin/bash ao && \
    mkdir -p /data/ao /data/home && \
    chown -R ao:ao /data && \
    chmod 700 /data/ao /data/home

ENV HOME=/data/home \
    AO_DATA_DIR=/data/ao \
    AO_RUN_FILE=/data/ao/running.json \
    AO_HARNESS_UPDATE_CHECKS=off \
    LANG=C.UTF-8 \
    PATH=/opt/ao/daemon:$PATH

# Числово, за да могат runtime-ите, които налагат non-root, да го проверят;
# това е потребителят ao.
USER 10001:10001
WORKDIR /data

# 3011 е автентикираният remote-host listener. Loopback API-то на 3001 остава
# вътре в контейнера и се използва само за health check-а.
EXPOSE 3011
HEALTHCHECK --interval=30s --timeout=5s --start-period=30s --retries=3 \
    CMD curl --fail --silent --show-error --output /dev/null http://127.0.0.1:3001/healthz || exit 1

# tini препраща SIGTERM към daemon-а и прибира осиротелите дъщерни процеси
# (иначе приключилите git помощници остават като зомбита под daemon с PID 1).
# -s запазва прибирането, когато runtime-ът добавя собствен init, както прави
# `docker run --init`.
ENTRYPOINT ["/usr/bin/tini", "-s", "--", "/usr/local/bin/ao-worker-entrypoint.sh"]
