# syntax=docker/dockerfile:1

# AO hub image for Railway: the public web UI, the hub BFF, and the daemon
# that backs the hub's own board, in one service. The final stage extends the
# worker image (deploy/railway/worker/Dockerfile), so the daemon, its agent
# CLIs, and tini are the same as on a worker.
#
# Build context is the REPO ROOT. Build the worker image first, or point
# AO_WORKER_IMAGE at a published tag:
#   docker build -f deploy/railway/worker/Dockerfile -t ao-worker:dev .
#   docker build -f deploy/railway/hub/Dockerfile -t ao-hub:dev .
#   docker build -f deploy/railway/hub/Dockerfile \
#     --build-arg AO_WORKER_IMAGE=ghcr.io/<owner>/ao-worker:<tag> -t ao-hub:dev .
#
# Runtime configuration is set on the service, never baked in. The operator
# sets AO_WEB_PASSWORD_HASH (from `npm run hash-password`); see
# packages/hub-bff/README.md for the rest. The hub's own daemon password is
# generated on first start and stays in the volume.

ARG AO_WORKER_IMAGE=ao-worker:dev
ARG NODE_IMAGE=node:24-bookworm-slim
ARG TARGETARCH

# ---- stage 1: the web build (platform independent output) ----
FROM --platform=$BUILDPLATFORM ${NODE_IMAGE} AS web
WORKDIR /src
COPY frontend/package.json frontend/package-lock.json ./frontend/
# Electron's postinstall download is not needed to build the web target.
RUN npm --prefix frontend ci --ignore-scripts
COPY frontend ./frontend
# The renderer imports the shared packages (product-ui, shared, cloud-client)
# from the repo tree, so the web build needs all of packages/.
COPY packages ./packages
RUN npm --prefix frontend run build:web && \
    test -f frontend/dist-web/index.html

# ---- stage 2: the hub on top of the worker image ----
FROM ${AO_WORKER_IMAGE} AS hub
# AO_HUB_SPA_DIR must contain index.html; the BFF refuses to start otherwise.
ENV AO_HUB_SPA_DIR=/opt/ao/hub/dist-web \
    AO_HUB_DATA_DIR=/data/hub \
    AO_HUB_DAEMON_PASSWORD_FILE=/data/ao/mobile/config.json \
    PORT=8080

# Install the pinned static Tailscale binaries. The archive is verified against
# the release checksum before either executable is copied into the image.
ARG TARGETARCH
ARG TAILSCALE_VERSION=1.104.1
ARG TAILSCALE_SHA256_AMD64=108d1d96ecf410d305571e173516f27038f870919a9b89c914a167c6d33a4528
ARG TAILSCALE_SHA256_ARM64=f60294374967f3dfd8cf57bbbd474d6cfce32d123e3a8ddeab82a87940daa806
USER root
RUN set -eu; \
    case "$TARGETARCH" in \
      amd64) tailscale_arch=amd64; tailscale_sha256="$TAILSCALE_SHA256_AMD64" ;; \
      arm64) tailscale_arch=arm64; tailscale_sha256="$TAILSCALE_SHA256_ARM64" ;; \
      *) echo "unsupported Tailscale architecture: $TARGETARCH" >&2; exit 1 ;; \
    esac; \
    archive="tailscale_${TAILSCALE_VERSION}_${tailscale_arch}.tgz"; \
    curl --fail --silent --show-error --location "https://pkgs.tailscale.com/stable/${archive}" --output "/tmp/${archive}"; \
    case "$tailscale_sha256" in *[!0-9a-f]*|'') echo "invalid Tailscale checksum" >&2; exit 1 ;; esac; \
    test "${#tailscale_sha256}" -eq 64; \
    printf '%s  %s\n' "$tailscale_sha256" "/tmp/${archive}" | sha256sum --check --status; \
    tar -xzf "/tmp/${archive}" -C /tmp; \
    install -m 0755 "/tmp/tailscale_${TAILSCALE_VERSION}_${tailscale_arch}/tailscale" /usr/local/bin/tailscale; \
    install -m 0755 "/tmp/tailscale_${TAILSCALE_VERSION}_${tailscale_arch}/tailscaled" /usr/local/bin/tailscaled; \
    rm -rf "/tmp/${archive}" "/tmp/tailscale_${TAILSCALE_VERSION}_${tailscale_arch}"

# The BFF has no runtime dependencies (node:http only) and Node 24 runs the
# .ts sources directly, so there is no npm install step for it.
COPY --chown=10001:10001 --from=web /src/frontend/dist-web /opt/ao/hub/dist-web
COPY --chown=10001:10001 packages/hub-bff/package.json /opt/ao/hub/bff/package.json
COPY --chown=10001:10001 packages/hub-bff/src /opt/ao/hub/bff/src
COPY --chown=10001:10001 packages/hub-bff/login /opt/ao/hub/bff/login
COPY --chmod=0755 deploy/railway/hub/hub-supervisor.sh /usr/local/bin/ao-hub-supervisor.sh

# The hub's own Claude Code and Codex, newer than the worker image's pins and
# kept updatable from Settings -> Harness. The worker's copies live in the
# root-owned /usr/local prefix, which the daemon (uid 10001) cannot update.
# These go to a prefix owned by uid 10001; the supervisor copies them into
# NPM_CONFIG_PREFIX on the volume, which PATH searches first, so an update
# made from the UI persists across restarts and a newer image version still
# replaces an older copy.
ARG HUB_AGENT_NPM_PACKAGES="@anthropic-ai/claude-code@2.1.302 @openai/codex@0.163.0"
# HOME=/root keeps npm's cache and the CLIs' --version state out of
# /data/home, which belongs to the volume. Login shells reset PATH from
# /etc/profile, so profile.d puts the updatable copies first there too.
USER root
RUN export HOME=/root && \
    npm install --global --prefix /opt/ao/npm-global ${HUB_AGENT_NPM_PACKAGES} && \
    /opt/ao/npm-global/bin/claude --version && \
    /opt/ao/npm-global/bin/codex --version && \
    chown -R 10001:10001 /opt/ao/npm-global && \
    rm -rf /root/.npm /root/.codex /root/.claude /root/.claude.json && \
    printf '%s\n' 'export PATH="/data/home/.npm-global/bin:/opt/ao/npm-global/bin:$PATH"' \
        >/etc/profile.d/ao-hub-agents.sh
USER 10001:10001
ENV AO_HUB_NPM_SEED=/opt/ao/npm-global \
    NPM_CONFIG_PREFIX=/data/home/.npm-global \
    PATH=/data/home/.npm-global/bin:/opt/ao/npm-global/bin:$PATH \
    AO_HARNESS_UPDATE_CHECKS=on

# The orchestrator wake-up loop (AO_HUB_ORCHESTRATOR_TICK=1, RW-5.4.2). Its
# invariant checks need python3, which the worker image does not ship.
USER root
RUN apt-get update && \
    apt-get install --yes --no-install-recommends python3 && \
    rm -rf /var/lib/apt/lists/* && \
    python3 --version
USER 10001:10001
COPY --chmod=0755 docs/backlog/railway/tools/orchestrator-tick.sh /usr/local/bin/ao-orchestrator-tick.sh

# Railway sets PORT; the BFF listens on it. The daemon's remote-host listener
# is on 3011 and loopback-only (see hub-supervisor.sh), so it is not exposed.
EXPOSE $PORT

# The BFF's /healthz is public and touches no daemon, so the check also asks the
# daemon's /readyz on the internal loopback API (3001).
HEALTHCHECK --interval=30s --timeout=5s --start-period=30s --retries=3 \
    CMD curl --fail --silent --show-error --output /dev/null "http://127.0.0.1:${PORT}/healthz" && \
        curl --fail --silent --show-error --output /dev/null http://127.0.0.1:3001/readyz

# tini reaps children and forwards SIGTERM to the supervisor, which stops both
# processes. If either exits, the supervisor exits and the container restarts.
ENTRYPOINT ["/usr/bin/tini", "-s", "--", "/usr/local/bin/ao-hub-supervisor.sh"]
