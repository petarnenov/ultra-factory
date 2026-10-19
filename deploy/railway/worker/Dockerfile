# syntax=docker/dockerfile:1

# AO worker image for Railway: the `ao` daemon, systemcheck's required tools,
# Node for Claude Code Chat, gh, the agent CLIs, tini and jq. Build from root.

ARG GO_IMAGE=golang:1.27.1-bookworm
# The ACP adapter needs Node 22+ on glibc 2.28+; Alpine (musl) does not work.
ARG NODE_IMAGE=node:24-bookworm-slim

# ---- stage 1: the ao binary, cross-compiled on the build host ----
FROM --platform=$BUILDPLATFORM ${GO_IMAGE} AS build
ARG TARGETOS=linux
ARG TARGETARCH
ARG AO_VERSION=dev
# A Railway repo build stamps its commit; an image build passes AO_COMMIT.
ARG RAILWAY_GIT_COMMIT_SHA=unknown
ARG AO_COMMIT=${RAILWAY_GIT_COMMIT_SHA}
WORKDIR /src/backend

COPY backend/go.mod backend/go.sum ./
RUN --mount=type=cache,id=s/0e7ebe89-bf3a-492c-a8f2-1a9ea22bc14c-/go/pkg/mod,target=/go/pkg/mod go mod download

COPY backend ./
# Pure-Go SQLite (modernc) builds with CGO disabled -> a static binary.
RUN --mount=type=cache,id=s/0e7ebe89-bf3a-492c-a8f2-1a9ea22bc14c-/go/pkg/mod,target=/go/pkg/mod \
    --mount=type=cache,id=s/0e7ebe89-bf3a-492c-a8f2-1a9ea22bc14c-/root/.cache/go-build,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath \
      -ldflags "-s -w -X github.com/aoagents/agent-orchestrator/backend/internal/cli.Version=${AO_VERSION} -X github.com/aoagents/agent-orchestrator/backend/internal/cli.Commit=${AO_COMMIT}" \
      -o /out/ao ./cmd/ao

# ---- stage 2: AO's patched Claude ACP adapter ----
# Platform-independent JS, built with the desktop's script and patches.
FROM --platform=$BUILDPLATFORM ${NODE_IMAGE} AS acp
WORKDIR /src/frontend
# npm retries transient fetch errors itself; CI has seen ECONNRESET outlive the
# default budget, so the adapter's install gets a larger retry count.
ENV NPM_CONFIG_FETCH_RETRIES=5 \
    NPM_CONFIG_FETCH_RETRY_MINTIMEOUT=20000 \
    NPM_CONFIG_FETCH_RETRY_MAXTIMEOUT=120000
COPY frontend/acp-runtime ./acp-runtime
COPY frontend/scripts/build-acp-runtime.mjs frontend/scripts/build-acp-runtime-helpers.mjs ./scripts/
RUN node scripts/build-acp-runtime.mjs && \
    test -f resources/acp-runtime/node_modules/@agentclientprotocol/claude-agent-acp/dist/index.js && \
    rm -rf resources/acp-runtime/node resources/acp-runtime/.ao-acp-runtime.json

# ---- stage 3: the worker ----
FROM ${NODE_IMAGE} AS worker
ARG TARGETARCH
# npm packages installed globally, each pinned. Override the whole list to add
# or drop a CLI; Claude Code must stay because Claude Code Chat runs it.
ARG AGENT_NPM_PACKAGES="@anthropic-ai/claude-code@2.1.302 @openai/codex@0.163.0 opencode-ai@1.19.3"
ARG SKIP_AGENT_INSTALL=0
# Optional; build with --build-arg CURSOR_AGENT_VERSION= to leave it out.
ARG CURSOR_AGENT_VERSION=2026.08.11-e8db854
SHELL ["/bin/bash", "-o", "pipefail", "-c"]

# Same npm retry budget for the global installs below; the hub stage, which is
# FROM this stage, inherits it for its own agent installs.
ENV NPM_CONFIG_FETCH_RETRIES=5 \
    NPM_CONFIG_FETCH_RETRY_MINTIMEOUT=20000 \
    NPM_CONFIG_FETCH_RETRY_MAXTIMEOUT=120000

# gh comes from GitHub's official apt repository, which only serves the latest
# release, so its version follows the build date rather than a pin.
RUN apt-get update && \
    apt-get install --yes --no-install-recommends \
        bash \
        ca-certificates \
        curl \
        git \
        jq \
        openssh-client \
        procps \
        sqlite3 \
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

RUN if [ "${SKIP_AGENT_INSTALL}" != "1" ]; then \
        npm install --global ${AGENT_NPM_PACKAGES} && \
        npm cache clean --force && \
        claude --version; \
    fi

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
# The daemon requires acp-runtime/node/bin/node; point it at the image's Node.
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

# Numeric so runtimes that enforce non-root can verify it; this is user ao.
USER 10001:10001
WORKDIR /data

# 3011 is the authenticated remote-host listener. The loopback API on 3001
# stays inside the container and is only used for the health check.
EXPOSE 3011
HEALTHCHECK --interval=30s --timeout=5s --start-period=30s --retries=3 \
    CMD curl --fail --silent --show-error --output /dev/null http://127.0.0.1:3001/healthz || exit 1

# tini forwards SIGTERM and reaps orphaned children; -s keeps reaping when the
# runtime adds its own init, as `docker run --init` does.
ENTRYPOINT ["/usr/bin/tini", "-s", "--", "/usr/local/bin/ao-worker-entrypoint.sh"]
