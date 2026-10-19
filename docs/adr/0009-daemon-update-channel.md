# 9. Daemon update channel and pull updater

Date: 2026-10-18
Status: Accepted (human approval: option C approved by the user, 2026-10-18)

## Context

Four self-hosted hosts (a Mac, 126, 164, and 223) plus the Railway hub run an
AO daemon. Every upgrade so far was manual: build from a checkout, copy the
binary over, restart the service. Three problems came out of that.

- **Skew.** Nothing tied a host to the commit it ran. The Railway hub reported
  `commit 0a954358c` while the branch it was deployed from was at `dcb89c55f`,
  18 commits ahead, missing `ProjectConfig.TerminateOnPRMerge` and the
  `SpawnSessionRequest.TerminateOnPRMerge` override. Debugging a daemon that
  does not contain the code it is supposed to contain is expensive. The image
  side of that failure is fixed by building the hub from the same commit as the
  worker; the host side still had no channel at all.
- **No record and no way back.** A host could not say which commit it was
  running, and swapping a binary left no copy of the previous one.
- **No distribution path.** `make daemon-dist` writes versioned archives into
  `dist/daemon/` that reach nothing. The one automated destination,
  `/releases/latest`, drives electron-updater for the desktop app and must not
  carry server builds.

Constraints found in the deployment and the code:

- **Hosts are pull-only.** They sit behind NAT on a home LAN or a tailnet and
  accept no inbound connection from the operator's machine. Anything that
  pushes updates needs the host to be reachable or an agent on it.
- **The operator wants zero touch.** The requirement was that a machine
  converge on the current build by itself, without an ssh session per upgrade.
- **Sessions hold work.** Replacing the daemon under a session that is running
  an agent destroys that work. `activity.state` is the durable fact available
  (`active`, `waiting_input`, and `blocked` are the states that matter).
- **Layouts differ per host today.** The Mac runs a bare binary in
  `~/.local/bin/ao` under launchd, 164 runs a bundle under systemd, 126 runs a
  release directory under launchd. A single installer must handle all three.
- **Rollback must be offline.** By the time a new daemon fails to become ready,
  the host may not have working network or DNS.
- **Assets are public.** This repository is public, so downloads need no
  credential and adding one would be a step that can break silently.
- **The desktop feed is sacred.** `release-latest-guard.yml` exists because a
  stray stable release breaks electron-updater for every desktop user.

## Options

| | **A: rebuild on each host** | **B: push from one machine** | **C: channel + pull updater** |
| --- | --- | --- | --- |
| Trigger | A human ssh session per host | A human or cron on one machine reaching every host | A timer on each host |
| Reachability | None beyond ssh | The updater machine must reach every host (LAN, tailnet, or a tunnel) | None; hosts dial out |
| Version source | Whatever the checkout on that host is | Whatever the updater machine built | The published manifest's commit |
| Rollback | Manual, if a copy was kept | Push the old build again | `prev` slot on the host, offline |
| Busy sessions | Operator decides | Operator decides | Session-state gate with a grace window |
| New failure mode | Hosts drift by commit again | Updater machine becomes a single point of failure; agents on hosts | A bad channel publish reaches every host on its next tick |

A rebuilds what we already had, so it does not fix skew. B removes the per-host
build but adds a new reachability requirement in the opposite direction from
the way the deployment is shaped, and it duplicates what the release pipeline
already does. C puts the version in one published artifact and lets each host
converge on it without any inbound path.

## Decision details

### 1. One rolling prerelease per channel

`Daemon update channel` (`.github/workflows/daemon-channel.yml`) publishes to a
tag named `daemon-channel-<channel>` and marks it a prerelease, so it can never
become `/releases/latest`. The channel name comes from the branch slug
(`feat/railway-hosting` → `feat-railway-hosting`) or from a manual dispatch
input, which also lets one publish serve a different ref.

Publishing runs on a push to `main` that touches the daemon, the
ACP runtime, or the updater's own files, and on `workflow_dispatch` (the latter
remains the way to publish a feature-branch channel). Pull
requests run the updater tests but publish nothing.

Assets are replaced in place on every publish and the names are version-free,
so a host's manifest URL is stable across upgrades:

- `ao-<os>-<arch>.tar.gz` for `darwin/arm64`, `darwin/amd64`, `linux/amd64`,
  and `linux/arm64`;
- `SHA256SUMS`, which also covers the updater script;
- `manifest.json`;
- `ao-self-update.sh`, so a new host can bootstrap with a single `curl`.

### 2. The manifest is the contract

`scripts/build-daemon-channel.sh` wraps `scripts/build-daemon-dist.sh`, renames
the per-version archives to the stable names, and writes the checksums and the
manifest. `build-daemon-dist.sh` now also writes `build.json` beside its
archives, and the channel script takes the version, commit, and build time from
it rather than re-deriving them, so there is one place that stamps a build.

```json
{
  "channel": "main",
  "version": "0.13.0",
  "commit": "dcb89c55f",
  "builtAt": "2026-10-18T15:59:15Z",
  "assets": {
    "linux-amd64": {
      "file": "ao-linux-amd64.tar.gz",
      "sha256": "…",
      "size": 13293746
    }
  }
}
```

A host reads `assets[<os>-<arch>]`, so one release serves every platform and a
host never has to guess a versioned name. The manifest is the only thing a host
trusts for the target commit.

### 3. The updater is a pull, not a daemon

`scripts/ao-self-update.sh` is a self-contained bash script, installed at
`~/.ao/bin/ao-self-update` and run by a systemd user timer or a LaunchAgent.
It talks only to the manifest URL and the release assets, opens no listener,
and needs no credential. Bash 3.2 on macOS is the floor; `python3`, `curl`, and
`tar` are the only tools it requires.

`status`, `check`, `apply`, `rollback`, and `version` are the commands. Exit
codes are part of the contract because the timer and a human read them: 0 for
current or updated, 1 for failure, 3 for a deferred busy host, 4 for disabled,
and 10 for "update available" from `check`.

### 4. Three layouts, detected not declared

`scripts/install-self-update.sh` reads `executablePath` from `ao status --json`,
falls back to `command -v ao`, and classifies the host from that path:

| Layout | Detection | Update |
| --- | --- | --- |
| `bundle` | `.../<dir>/daemon/ao` without a `releases/` parent | replace `daemon/`, `acp-runtime/`, `tmux/` in place |
| `release` | `.../<root>/releases/<stamp>/resources/daemon/ao` | build a new `<root>/releases/<stamp>/` and move `current` |
| `binary` | anything else | replace the single binary |

Detection is not guessed from a flag because the three hosts in use today
genuinely differ, and a wrong layout at 3 a.m. is a broken host. A layout can
still be forced with `--layout` when detection cannot work, for example over an
already-broken install.

The installer also finds the service that runs the daemon: a systemd user unit
whose `ExecStart` mentions the binary, or a LaunchAgent whose
`ProgramArguments[0]` resolves to it. It writes `~/.ao/self-update.json`,
including any `AO_DATA_DIR`, `AO_RUN_FILE`, or `AO_PORT` in its environment, so
the updater's `ao` calls agree with the service. If no service matches, the
installer stops and asks for `--unit` or `--label` instead of writing a config
that cannot restart anything.

### 5. Gates, verification, and rollback

`apply` takes the session gate before it touches anything: with any
non-terminated session in `active`, `waiting_input`, or `blocked` whose
`lastActivityAt` is inside `graceMinutes` (20 by default), it exits 3 and the
timer retries on its next tick. An unreachable session list is treated as
"not busy, keep going" and logged: a broken daemon must not be able to block
its own repair. `--force` is the operator override.

A channel is authoritative in the forward direction only. When the manifest's
`builtAt` predates the installed build's, the host is ahead of the channel and
`apply` records `channel-behind` and changes nothing, because the alternative
is a silent downgrade to a build without whatever the host was upgraded for.
Comparing commits cannot establish order without history on the host, so the
absolute build time is what the check uses. `--force` still installs it.

After the swap the host must prove the new build works before it keeps it:

1. the staged `ao version` must report the commit the manifest pins, so a
   truncated or mismatched archive never reaches the install directory;
2. the service restarts, and `ao status --json` must report `ready` with an
   `executablePath` under the install directory within the ready timeout.

If either fails, the previous build is restored, the service restarts again,
and the failed build is kept under `~/.ao/self-update/failed/<commit>`. This is
the whole reason for the `prev` slot: the recovery path is on the host and does
not need the network that just failed.

### 6. Layered with the image fix, not a replacement

This channel covers self-hosted daemons. The Railway hub and workers keep
updating through their container images, which now build the hub from the same
commit as the worker. The two mechanisms share the definition of "the commit a
build came from" but nothing else; neither is a fallback for the other.

### 7. The one workflow allowed to touch releases

`frontend/release-workflows.test.ts` forbids every public workflow from
mutating releases or release tags, because a second publisher of the desktop
app once made a release unreadable and left the update feed ambiguous. That
rule protects the desktop feed, which a daemon channel is not part of, so
`daemon-channel.yml` is the single exemption.

The exemption is checked, not declared. The guard stops applying its blanket
rule to that one file, and a second test requires the file to create and edit
releases only as prereleases, to use `daemon-channel-<channel>` tags, and never
to touch `latest` or a `desktop-v*` tag. `release-latest-guard.yml` keeps
asserting that `/releases/latest` is a stable desktop release, so a stray
channel publish cannot take the feed over.

## Consequences

- A host's daemon commit is published, verifiable, and recorded, so "which
  build is 223 running" is answered by `ao-self-update status` instead of an
  ssh session.
- Upgrades no longer need inbound access to a host, and the four existing hosts
  can be brought in line with one command each.
- A bad publish reaches every host that follows the channel within one tick.
  The mitigations are the staged-commit check, the readiness check, the `prev`
  slot, the hourly (not immediate) cadence, and the fact that channels are
  branch-scoped and published only from `main` and `feat/**`.
- An updater failure is silent by nature: nobody is watching a timer. The
  outcome is recorded in `state.json` and the timer's log, and `check` exists
  for an external monitor to poll.
- The channel duplicates platform knowledge that `build-daemon-dist.sh`
  already has; it is kept to a wrapper plus a manifest writer to limit drift.
- `tmux` is deliberately outside the channel. A host that needs a newer tmux
  still installs it by hand, and an update never removes what the host chose to
  have.

## Open questions

- Should a channel release also carry the desktop app's assets so one machine
  can serve both? No for now: the desktop has its own release feed and
  electron-updater logic.
- Should `apply` restart the daemon when only the ACP runtime changed? Today it
  always restarts, which is simpler and costs one restart.
- Should the timer back off after repeated failures? Exit 1 is logged and the
  next tick is an hour away, which is already a slow retry.

## Verification

- `scripts/ao-self-update.test.sh` covers `status`, `check`, all three layouts,
  the busy and idle gates, a checksum mismatch, a channel build older than the
  installed one, an update that never reports ready (rollback), and an explicit
  `rollback`. It runs in the `Daemon update channel` workflow on every pull
  request that touches these files.
- The installer was run on the Mac in `--dry-run` and against a scratch `HOME`
  with a fake `ao` and a fake `LaunchAgent`, and it classified the real Mac as
  `binary` in `~/.local/bin` with `dev.ao.daemon` as its service.
- A real `darwin/arm64` channel was staged, and `apply` installed it into a
  scratch `HOME`, restarted a real daemon from the new build, and recorded
  `outcome: updated` after `ao status --json` reported `ready` from the
  installed path.
- The channel's manifest, `SHA256SUMS`, and asset names were generated and
  read back by the updater, not just written.
- `frontend/release-workflows.test.ts` fails if any public workflow except
  `daemon-channel.yml` mutates releases or tags, and fails if that one stops
  being a prerelease-only, `daemon-channel-*`-tagged publisher.
