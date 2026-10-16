# AO worker image (Railway)

A container image that runs the AO daemon as a remote worker host. It holds
the `ao` binary (pure Go, `CGO_ENABLED=0`), everything `systemcheck` marks
Required (`git`, `tmux`), Node 24 with AO's patched Claude ACP adapter for
Claude Code Chat, `gh`, and the agent CLIs.

## Build

The build context is the repository root. `Dockerfile.dockerignore` limits the
context to the files the image uses.

```bash
docker build -f deploy/railway/worker/Dockerfile -t ao-worker:dev .
docker run --rm ao-worker:dev sh -c 'ao version && git --version && tmux -V && node -v && claude --version && gh --version'
```

Multi-arch with buildx. The Go binary and the ACP adapter build on the build
host; only the final stage runs under emulation for the other architecture:

```bash
docker buildx build --platform linux/amd64,linux/arm64 \
  -f deploy/railway/worker/Dockerfile -t ao-worker:dev .
```

A `docker-container` buildx builder runs arm64 steps with BuildKit's bundled
QEMU. On a builder without emulation, register it once on the host with
`docker run --privileged --rm tonistiigi/binfmt --install arm64`.

Build arguments:

| Argument | Default | Purpose |
|---|---|---|
| `AGENT_NPM_PACKAGES` | `@anthropic-ai/claude-code@2.1.228 @openai/codex@0.147.0 opencode-ai@1.19.3` | Pinned global npm installs. Keep Claude Code in the list. |
| `CURSOR_AGENT_VERSION` | `2026.08.11-e8db854` | Cursor Agent release. Set it empty to leave Cursor out. |
| `AO_VERSION`, `AO_COMMIT` | `dev`, `unknown` | Stamped into `ao version`. |
| `GO_IMAGE`, `NODE_IMAGE` | `golang:1.27.1-bookworm`, `node:24-bookworm-slim` | Base images. |

`gh` comes from GitHub's official apt repository. That repository serves only
the latest release, so the `gh` version follows the build date.

## Layout and environment

| Path or variable | Value |
|---|---|
| `/opt/ao/daemon/ao` | daemon and CLI, on `PATH` |
| `/opt/ao/acp-runtime` | patched `claude-agent-acp`; `node/bin/node` links to the image's Node |
| `/usr/local/bin/ao-worker-entrypoint.sh` | entrypoint |
| user | `ao`, uid/gid 10001 |
| `HOME` | `/data/home` |
| `WORKDIR` | `/data` |
| `AO_DATA_DIR` | `/data/ao` |
| `AO_RUN_FILE` | `/data/ao/running.json` |
| `AO_HARNESS_UPDATE_CHECKS` | `off` |
| `EXPOSE` | `3011` (remote-host listener) |
| `HEALTHCHECK` | `GET http://127.0.0.1:3001/healthz` inside the container |

The daemon finds `acp-runtime/` next to `daemon/`, the same layout as
`make daemon-dist`, so `AO_ACP_RUNTIME_DIR` is not needed. `tmux` comes from
Debian and is found on `PATH`, so `AO_TMUX_BINARY` is not needed either.

## Size

Measured 2026-10-16 from `main` @ f421a7098 with the defaults above:

| Platform | Compressed (registry layers) | Unpacked |
|---|---|---|
| linux/amd64 | 559 MiB | 2.2 GB |
| linux/arm64 | 490 MiB | not measured |

The target is under 1.5 GB compressed. The agent CLIs are most of it: the npm
layer is about 1 GB unpacked (Claude Code alone is about 300 MB).

## Entrypoint

`ao-worker-entrypoint.sh` is a placeholder. It runs the given command, or
`ao daemon` in the foreground when there is none. RW-1.2.2 replaces it with
the real entrypoint: volume setup, remote-host pre-seed, and credentials from
env. Until then the remote-host listener on 3011 is not enabled.

Known gaps for RW-1.2.2:

- `ao daemon` runs as PID 1 and does not reap orphaned children, so exited
  `git` processes stay as zombies. Run under `tini` or `docker run --init`.
- Railway mounts volumes owned by root ([docs](https://docs.railway.com/volumes)).
  The image runs as uid 10001, and ownership set at build time does not carry
  over to the volume. Either the service sets `RAILWAY_RUN_UID=0` and the
  entrypoint chowns `/data` and drops to uid 10001, or `/data` must already be
  writable for uid 10001.
