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
# sets AO_HUB_OPERATOR_PASSWORD_HASH (from `npm run hash-password`); see
# packages/hub-bff/README.md for the rest. The hub's own daemon password is
# generated on first start and stays in the volume.

ARG AO_WORKER_IMAGE=ao-worker:dev
ARG NODE_IMAGE=node:24-bookworm-slim

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

# The BFF has no runtime dependencies (node:http only) and Node 24 runs the
# .ts sources directly, so there is no npm install step for it.
COPY --chown=10001:10001 --from=web /src/frontend/dist-web /opt/ao/hub/dist-web
COPY --chown=10001:10001 packages/hub-bff/package.json /opt/ao/hub/bff/package.json
COPY --chown=10001:10001 packages/hub-bff/src /opt/ao/hub/bff/src
COPY --chown=10001:10001 packages/hub-bff/login /opt/ao/hub/bff/login
COPY --chmod=0755 deploy/railway/hub/hub-supervisor.sh /usr/local/bin/ao-hub-supervisor.sh

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
