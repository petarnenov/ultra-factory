# Self-hosted remote hosts (experimental)

Sessions stay on the machine that started them. Desktop and mobile are clients.
AO account sign-in is required for desktop remote hosts; Cloud sandbox execution is separate.

## Install on Ubuntu

On a fresh Ubuntu 24.04 x64 machine, SSH in as a non-root user with `sudo`:

```bash
ssh -i /path/to/private-key USER@HOST_ADDRESS
```

Run **one** command on the VM. Each installs AO, prerequisites, a persistent
user service, and a Cloudflare quick tunnel, then prints the connection details.

**Nightly users** (installs the newest published nightly without building from source):

```bash
bash -c 'set -o pipefail; sudo apt-get update && sudo apt-get install -y curl && curl -fsSL https://raw.githubusercontent.com/Untrivial-ai/agent-orchestrator/main/scripts/bootstrap-self-hosted.sh | bash -s -- --nightly'
```

Use a nightly desktop app with a nightly host. Updating your laptop alone does
not update the VM. Until stable includes account linking, use this command for
the pair-once, automatic-discovery flow below. Nightlies are experimental.

**Testing an unreleased branch** (builds from that branch): replace
`codex/your-pr-branch` with the branch name published on GitHub.

```bash
SOURCE_REF=codex/your-pr-branch bash -c 'set -o pipefail; sudo apt-get update && sudo apt-get install -y curl && curl -fsSL "https://raw.githubusercontent.com/Untrivial-ai/agent-orchestrator/${SOURCE_REF}/scripts/bootstrap-self-hosted.sh" | bash -s -- --source-ref "${SOURCE_REF}"'
```

**Stable users** (installs the latest published stable binary, not nightly):

```bash
bash -c 'set -o pipefail; sudo apt-get update && sudo apt-get install -y curl && curl -fsSL https://raw.githubusercontent.com/Untrivial-ai/agent-orchestrator/main/scripts/bootstrap-self-hosted.sh | bash'
```

For a trusted LAN/VPN instead, append `--lan` to the script arguments
(`bash -s -- --lan` for the release command). This uses plaintext HTTP; never
expose the port publicly. The VM must stay powered on.

Re-run the command to upgrade without deleting sessions. The installer uses
`~/.ao/host`; it does not copy agent or GitHub credentials from your laptop.

## Check and connect

On the VM, get the current address and password (keep it private):

```bash
~/.local/bin/ao remote-host status
```

If the HTTPS address is not ready, rerun the command after a few seconds.

For private Git clones, pushes, or PRs, authenticate on the VM as the AO user:

```bash
gh auth login
gh auth setup-git
gh auth status
```

Pairing does not copy GitHub credentials from your laptop.

1. On one laptop, turn on **Developer mode** in **Settings → General** and sign
   in to AO Cloud. In **Settings → Remote hosts**, add the VM's name, exact
   `Address:` and `Password:`. Alternatively, sign in on mobile under
   **Settings → Account** and pair it once under **Settings → Machines**.
2. Sign in to the same AO account on your other laptops and phones. Hosts,
   projects, and sessions load automatically; enable **Developer mode** on each
   desktop client. A running client may take up to 30 seconds to find a newly
   linked host.
3. Use **Projects +** to clone or import a project on the VM. In desktop
   **Settings → Harness**, select the VM to install/sign in to an agent there.

The host publishes a changed quick-tunnel address to AO Cloud automatically.
Desktop connections saved before account linking keep their original pairing
password and are not attributed to whichever account signs in next. Re-pair
only if you want to replace that local credential with an account-scoped one.

## Status and other hosts

On the host, use `~/.local/bin/ao status` for daemon status,
`systemctl --user status ao-self-hosted.service --no-pager` for the service,
or `journalctl --user -u ao-self-hosted.service -n 100 --no-pager` for logs.
Run `~/.local/bin/ao remote-host disable` to stop remote access.

For macOS or another service manager, use the
[lower-level installer](../scripts/setup-self-hosted.sh); pass `--nightly` to
select the nightly channel. On macOS it installs
a LaunchAgent that runs only while the user is logged in; for a container,
use `--install-only` and supervise `ao daemon` yourself. Windows is not a
native host. Projects still need their own build dependencies on the host.

Removing a saved connection on desktop/mobile does not stop the host or its
sessions.

## Portable daemon without cloning

To run a host from a build of this checkout without cloning on the target
machine, run `make daemon-dist` on any macOS or Linux machine. It
cross-compiles `ao` for `darwin/arm64`, `darwin/amd64`, `linux/amd64`, and
`linux/arm64`. Each archive bundles the newest Node.js 24 LTS (verified against
nodejs.org checksums) and AO's patched `claude-agent-acp`, so Claude Code Chat
works on the host. The output is `dist/daemon/ao-<version>-<os>-<arch>.tar.gz`
plus `SHA256SUMS`.

Copy one archive to the host, then follow the `README.txt` inside it. The
daemon finds `acp-runtime/` next to `daemon/ao` on its own:

```bash
mkdir -p ~/.local/share/ao
tar -xzf ao-<version>-linux-amd64.tar.gz -C ~/.local/share/ao --strip-components=1
export PATH="$HOME/.local/share/ao/daemon:$PATH"
ao daemon &   # or under tmux, launchd, or systemd
ao remote-host enable
```

Options:

- `DAEMON_PLATFORMS="linux/amd64"` builds a subset.
- `DAEMON_NODE_VERSION=24.x.y` pins a Node release.
- `DAEMON_NODE=0` ships the bare static binary without Node; Claude Code then
  works in Terminal UI mode only.

The archive does not install a service or tmux. The bundled Linux Node needs
glibc 2.28 or newer.

## Automatic updates

A host can follow a rolling update channel instead of being upgraded by hand.
Each channel is one GitHub **prerelease**, `daemon-channel-<channel>`, whose
assets are replaced in place on every publish:

- `ao-<os>-<arch>.tar.gz` — the `daemon/` and `acp-runtime/` layout
  `make daemon-dist` produces, under a version-free name;
- `SHA256SUMS` — SHA-256 for every asset, including the two scripts below;
- `manifest.json` — channel, version, commit, and each asset's file name,
  size, and SHA-256;
- `ao-self-update.sh` and `install-self-update.sh` — the pull updater and the
  installer, so a new host can bootstrap from the release alone:

```bash
curl -fsSLO https://github.com/<owner>/<repo>/releases/download/daemon-channel-main/install-self-update.sh
curl -fsSLO https://github.com/<owner>/<repo>/releases/download/daemon-channel-main/ao-self-update.sh
bash install-self-update.sh --install --channel main --repo <owner>/<repo>
```

Set a host up with the installer, which detects the install layout and the
service that runs the daemon:

```bash
scripts/install-self-update.sh --install --channel main --repo <owner>/<repo>
```

From a checkout the same thing is `make daemon-update-install
DAEMON_CHANNEL=main` (the repository defaults to this checkout's `origin`),
`make daemon-update DAEMON_UPDATE_CMD=status` reports a host, and
`make daemon-channel` stages a channel locally.

It writes `~/.ao/self-update.json`, copies the updater to
`~/.ao/bin/ao-self-update`, and enables a systemd user timer or a LaunchAgent
that runs `ao-self-update apply` every hour (`--interval-minutes`). On systemd
hosts run `loginctl enable-linger <user>` once so the timer also fires while
logged out. Re-running it refreshes the config and the installed updater.

| Layout | Install directory | What an update replaces |
| --- | --- | --- |
| `bundle` | `~/.local/share/ao` | `daemon/`, `acp-runtime/`, and `tmux/` when present |
| `release` | `~/.ao/host`, through `current/resources` | a new `releases/<stamp>/`, then `current` moves to it |
| `binary` | the directory holding `ao` | that single binary |

Channel bundles carry only the daemon and the ACP runtime, so a host keeps the
`tmux` it already has.

`ao-self-update apply` does the following:

1. Read the channel manifest; compare its commit with `ao version` of the
   installed binary and stop when they already match. A channel build whose
   `builtAt` predates the installed build is also left alone, so a host pointed
   at a lagging channel is never downgraded; `--force` overrides that.
2. Defer (exit 3) while a session is `active`, `waiting_input`, or `blocked`
   and was last active within `graceMinutes`. `--force` skips this gate, and a
   stopped daemon is never busy.
3. Download this platform's asset, check its size and SHA-256, and extract it
   under `~/.ao/self-update/staging`.
4. Run the staged `ao version` and require the commit the manifest pins.
5. Move the current build to `~/.ao/self-update/prev`, install the staged one,
   and restart the service.
6. Wait for `ao status --json` to report `ready` from that binary. If it does
   not within `--ready-timeout` seconds (60), restore `prev`, restart again,
   and exit 1 with the failed build kept under
   `~/.ao/self-update/failed/<commit>`.

```bash
~/.ao/bin/ao-self-update status     # installed and available commits
~/.ao/bin/ao-self-update check      # exit 10 when an update is waiting
~/.ao/bin/ao-self-update apply      # update now
~/.ao/bin/ao-self-update rollback   # put the previous build back
```

Exit codes: 0 for current or updated, 1 for failure, 3 for a deferred busy
host, 4 for disabled, 10 for "update available" from `check`. Each run records
its outcome in `~/.ao/self-update/state.json`.

The updater only pulls. It opens no inbound port and needs no credential
because the release assets of this repository are public. A channel release is
always a prerelease and never touches `/releases/latest`, which stays reserved
for the desktop app. Publishing happens on a push to `main` that touches the
daemon or the updater, or from a manual run of the `Daemon update channel`
workflow (its `ref` and `channel` inputs are the way to publish a
feature-branch channel).

```json
{
  "channel": "main",
  "repo": "<owner>/<repo>",
  "enabled": true,
  "layout": "bundle",
  "installDir": "/home/ao/.local/share/ao",
  "graceMinutes": 20,
  "service": { "kind": "systemd", "unit": "ao-self-hosted.service" },
  "env": { "AO_DATA_DIR": "/home/ao/.ao" }
}
```

The installer writes that file and it can be edited by hand.
`manifestUrl` replaces `repo` for a channel that is not a GitHub release.
`busyStates` replaces the default deferral set. `service.kind` is `systemd`,
`launchd`, or `command`, where `command` runs `service.restartCommand`.
`env` is passed to every `ao` call the updater makes, which matters when the
service sets `AO_DATA_DIR`, `AO_RUN_FILE`, or `AO_PORT`.

## Behind a reverse proxy

The remote endpoint locks a source out for a minute after five wrong
passwords. It keys that lockout on the TCP peer. Behind a TLS-terminating HTTP
proxy, such as a Railway public domain, every request arrives from the proxy,
so one client guessing wrong passwords locks out every other client.

For that deployment only, start the daemon with:

```bash
AO_REMOTE_TRUSTED_PROXY=1 ao daemon
```

The listener then keys the lockout on the **rightmost** `X-Forwarded-For`
entry, the one the nearest proxy appended. Earlier entries are
client-controlled and are ignored. If the rightmost entry is not a valid IP,
the TCP peer is used. The loopback `CF-Connecting-IP` rule for the managed
Cloudflare tunnel is unchanged and still applies first. The forwarded address
never authorizes anything: every request except `GET /api/v1/identity` still
needs the password or account credential.

Set it only when the proxy is the sole public path to the listener and exactly
one proxy sits in front of it. Never set it on a desktop or a LAN, Tailscale,
or quick-tunnel host: any client that reaches the port directly can then choose
its own lockout key and guess without limit. Services that reach the worker
over private networking without the proxy can do the same, so keep that
network to your own services. The decision and the full safe/dangerous table
are in [ADR 0005](adr/0005-trusted-proxy-remote-host.md).

With the flag on, the daemon logs one warning each time the listener starts.
In any mode, a source that becomes locked out logs one warning with the
derived key, the TCP peer, and whether the key came from `X-Forwarded-For`,
`CF-Connecting-IP`, or the peer.

## Security and limits

- AO's unauthenticated listener stays on `127.0.0.1`. The opt-in remote
  endpoint requires a password; `--lan` is plain HTTP, so use only a trusted
  LAN/VPN and never expose its port publicly.
- AO sign-in gates desktop discovery, not the VM's direct pairing endpoint.
  Pairing on one device issues a separate 256-bit host credential that AO Cloud
  stores encrypted and shares only with the signed-in account's devices. The
  original VM password is not uploaded. AO Cloud and signed-in devices can use
  the scoped credential to reach the VM; removing a saved host from an account
  does not invalidate a credential already imported on another device until it
  syncs. Pairing the VM to another account rotates the scoped credential.
  Regenerating the VM connection password revokes the scoped credential, so
  re-pair the host with the account afterward.
- Host-ID checks prevent connecting to the wrong host, but not an active
  network attacker or a copied AO data directory. Desktop passwords are stored
  in `~/.ao/remotes.json` (or `AO_DATA_DIR/remotes.json`) with owner-only access.
- Cloudflare terminates tunnel TLS and can see traffic. Quick tunnels have no
  uptime guarantee and change hostname on restart.
- AO can edit host files; opening them in a client-side external editor needs
  a separate remote workspace connection.
