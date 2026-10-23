# Distribution and releases

Loaded on demand from [`AGENTS.md`](../../AGENTS.md). Read it before touching install paths, release workflows, or macOS artifacts.

- The **desktop app** (GitHub Releases) is the canonical, auto-updating install path. Point users there first.
- **npm still works but is no longer recommended.** `0.10.0` is the final version published to npm; the `@aoagents/ao` package is frozen and will not receive further updates. It remains a legacy on-ramp for users who already have `ao` on their PATH, where `ao start` fetches and opens the desktop build. Do not add features, docs, or flows that treat npm as the intended way to install AO.
- **Exactly one publisher.** Only the designated release conductor runs a real publish, on any channel. Divergent artifacts from multiple publishers made the 28-29 Jul macOS incident unreadable. Use the fork dev loop for test builds. Full rule and rationale: `frontend/docs/desktop-release.md`, "Hard rule: exactly one publisher".
- **The daemon update channel is the one exception** to that rule: `.github/workflows/daemon-channel.yml` publishes prereleases under `daemon-channel-*` for self-hosted hosts, never a desktop artifact and never `/releases/latest`, and `frontend/release-workflows.test.ts` pins those properties. See `docs/adr/0009-daemon-update-channel.md`.
- **Verify macOS artifacts with `frontend/scripts/verify-mac-artifact.sh`, never by hand.** It extracts with `ditto -x -k` and runs `codesign --verify --deep --strict`, `spctl -a -vv -t exec`, `xcrun stapler validate`. Plain `unzip` breaks the seal and yields a convincing false failure; `spctl` without `-vv` prints nothing at all on success.
- **macOS ships both a `.zip` and a `.dmg`.** The dmg is first install only. The zip and `latest-mac.yml` must keep publishing forever: electron-updater cannot install an update from a dmg. macOS differential updates are permanently disabled (full download only); see issues #3151 and #3267.
