# AO worker image (Railway)

A container image that runs the AO daemon as a remote worker host. It holds
the `ao` binary (pure Go, `CGO_ENABLED=0`), everything `systemcheck` marks
Required (`git`, `tmux`), Node 24 with AO's patched Claude ACP adapter for
Claude Code Chat, `gh`, the agent CLIs, and `tini` and `jq` for the
entrypoint.

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
| `/usr/local/bin/ao-worker-entrypoint.sh` | entrypoint, run by `tini -s` |
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

`tini` is PID 1. It forwards `SIGTERM` to the daemon and reaps orphaned
children, which a PID 1 daemon leaves as zombies. `-s` keeps that working
when the runtime adds its own init (`docker run --init`). The entrypoint
(`ao-worker-entrypoint.sh`) runs on every start and then replaces itself with
the given command, or `ao daemon` in the foreground when there is none:

1. **As root**, it hands `/data`, `AO_DATA_DIR`, and `HOME` to uid 10001
   (`chown -R`, only when the top-level owner is wrong) and re-runs itself as
   uid 10001 with `setpriv`. The daemon never runs as root.
2. Creates `AO_DATA_DIR` and `HOME` (mode 0700). If they are not writable it
   exits with a hint about `RAILWAY_RUN_UID=0`.
3. **First start only** (no `<AO_DATA_DIR>/mobile/config.json`): writes
   `{"enabled":true,"password":...,"lastPort":...,"noPublicTunnel":true}`
   with mode 0600. The daemon restores the remote-host listener from it on
   boot, so no interactive `ao remote-host enable` is needed.
   `noPublicTunnel` is what `ao remote-host enable` saves. The password is
   `AO_REMOTE_PASSWORD`, or a generated 8-character one that is logged on
   that start only. Later starts keep the file as it is and log the password
   masked; `ao remote-host status` in the container shows it.
4. Arms or revokes the account token (below).
5. Sets the git identity and the `gh` credential helper from the environment.
6. `exec`s the command. For `ao daemon`, a detached helper waits for
   `/readyz` and logs `hostId` and the listener address and port.

`AO_ENTRYPOINT_DRY_RUN=1` runs steps 2-5 and prints the command instead of
running it. `entrypoint_test.sh` uses it to test the pre-seed logic:

```bash
deploy/railway/worker/entrypoint_test.sh
```

### Variables

| Variable | Used for |
|---|---|
| `AO_REMOTE_PASSWORD` | Remote-host password on the first start. Ignored once `config.json` exists; delete the file to re-seed. |
| `AO_REMOTE_PORT` | `lastPort` on the first start, default `3011`. The daemon falls back to an ephemeral port if it is taken; the startup log shows the real one. |
| `AO_REMOTE_ACCOUNT_TOKEN` | 256-bit Bearer credential for a public URL, for example `openssl rand -hex 32`. |
| `AO_GIT_USER_NAME`, `AO_GIT_USER_EMAIL` | `git config --global user.name/user.email` in `HOME`. |
| `GH_TOKEN` (or `GITHUB_TOKEN`) | Read by `gh` directly; the entrypoint also runs `gh auth setup-git` so `git` over HTTPS uses it. |
| `CLAUDE_CODE_OAUTH_TOKEN`, `ANTHROPIC_API_KEY`, `ANTHROPIC_AUTH_TOKEN` | Read by Claude Code directly, nothing is written. |
| `AO_PORT` | Loopback API port the startup helper polls, default `3001`. |

The startup log names which credential variables are set, never their
values.

**Account token.** The entrypoint writes `accountTokenHash`, the SHA-256 hex
of `AO_REMOTE_ACCOUNT_TOKEN`, into `config.json` on every start. The
listener accepts that token as a Bearer credential beside the password. The
daemon's `POST /api/v1/remote-host/account-token` is not used: it mints its
own random token and cannot take one from the caller. A daemon-issued
`accountToken`/`accountTokenOwner` is dropped when the variable replaces it.
When the variable is removed, the next start removes a hash that the
entrypoint wrote (a hash with no `accountToken` beside it); a token the
daemon issued is kept. Rotating the password in AO also revokes the token
until the next start writes it again.

### Railway

- Mount the volume at `/data`. Railway mounts volumes owned by root, and
  ownership set at build time does not carry over
  ([docs](https://docs.railway.com/volumes)). Set `RAILWAY_RUN_UID=0` on the
  service so the entrypoint starts as root, chowns the volume, and drops to
  uid 10001. Without it the container exits with an error about `/data/ao`.
- Leave the service's start command empty. Its default is the image's
  `ENTRYPOINT`, which runs `tini` and the pre-seed. Railway's docs do not say
  whether a custom start command keeps the `ENTRYPOINT`
  ([docs](https://docs.railway.com/reference/build-and-start-commands)).

Locally, a named volume (`-v vol:/data`) starts with the image's `/data`, which
is already owned by uid 10001, so `RAILWAY_RUN_UID` is not needed there. To
mimic Railway, run with `-u 0`.
