# ao host

List the remote AO hosts this machine can delegate work to with `--host`.

A remote host is another machine running AO with remote access on. Workers
spawned there run in that machine's worktree, with its agents, credentials, and
build tools, and are supervised by its daemon. The desktop shows them alongside
local sessions.

Hosts come from two places:

- **Tailscale (automatic).** Every AO daemon on the user's Tailscale network
  that has remote access on (`ao remote-host enable`) is found by this
  machine's daemon, with no AO Cloud account, password, or pairing. Its URL is
  always its Tailscale address (`http://100.x.y.z:3011`). The host admits this
  machine on its Tailscale identity, checked through `tailscaled`: by default
  only nodes of the same Tailscale user; `AO_TAILNET_ALLOW_USERS` and
  `AO_TAILNET_ALLOW_TAGS` on the host widen that. The label defaults to the
  Tailscale machine name and can be renamed or hidden in **Settings → Remote
  hosts**. This machine and its sibling AO installs never appear.
- **Paired by address (saved).** A host outside Tailscale, added in the desktop
  app under **Settings → Remote hosts** with its address and connection
  password.

## Syntax

```
ao host ls [--json]
```

`ao host ls` checks each host's identity first, then asks it with this
machine's Tailscale identity (Tailscale hosts) or the saved password (saved
hosts), and reports one status. The `VIA` column (`source` in JSON) says
`tailscale` or `saved`. A first line says **Tailscale unavailable** when
`tailscaled` on this machine could not be read; saved hosts still work then.

| Status | Meaning |
|---|---|
| `online` | Identity verified and the host accepted this machine. Usable with `--host`. |
| `unauthorized` | A Tailscale host does not allow this machine's Tailscale identity, or Tailscale is unavailable on it (the detail says which); a saved host rotated its password. |
| `offline` | Unreachable, remote access off, or not an AO daemon. A Tailscale host stays listed as offline until its node leaves the tailnet. |
| `identity-changed` | A different AO installation answers at that address. Do not use it. Ask the user to re-pair. |
| `needs-pairing` | Saved before identities were recorded. Ask the user to re-pair. |
| `this-machine` | The entry points at this machine. Omit `--host` instead. |

Passwords are never printed. A Tailscale host never receives one.

Hosts bind the well-known tailnet port 3011 (`AO_TAILNET_PORT`). A host that
had to bind another port is found only if that port is listed in
`AO_TAILNET_PROBE_PORTS` on the discovering machine.

## Commands that accept `--host`

`--host <label | host ID | URL>` runs the command against that host's daemon:

- `ao spawn`
- `ao send`
- `ao session ls | get | top | input | kill | restore | rename | exit-agent | resume-agent | claim-pr`
- `ao project ls | get`
- `ao orchestrator ls`
- `ao agent ls`

Other commands are local-only and reject the flag.

## Delegating to another machine

```bash
ao host ls                                         # pick an online host
ao project ls --host build-box                     # optional: see its projects
ao spawn --host build-box --name "fix flaky e2e" \
  --prompt "Fix the flaky checkout e2e test and open a PR."
ao session ls --host build-box                     # follow PR, CI, review, activity
ao session top --host build-box                     # memory and process count on that machine
ao send --host build-box --session widgets-3 --message "CI failed on lint; fix it."
```

- **Choosing the project:** without `--project`, `spawn --host` selects the
  remote project whose repository matches your current project (SSH and HTTPS
  remotes match). If no remote project or several share the repository, it
  stops and lists the remote project IDs. Pass `--project <remote-id>`.
- **Session IDs belong to one host:** `widgets-3` on a remote host is unrelated
  to `widgets-3` here. Always pass the same `--host` when you list, inspect,
  message, or kill a remote session.
- **No parent link:** a remote worker is not linked to the local orchestrator.
  Track it with `ao session ls --host <host>`, and record the host and session
  ID in your own notes or report.
- **Message attribution:** messages you send carry a text prefix such as
  `[from <your-session> on <this-machine>]`. Your session ID is not used as a
  remote sender.
- **Use full PR URLs:** with `--claim-pr`, prefer the full PR URL. A bare number
  needs the remote project's recorded repository.
- **Rejected over Tailscale:** an error naming this machine's Tailscale
  identity means the host's user or allowlist does not include this machine,
  or `tailscaled` is down on the host. Fix that on the host; there is no
  password to enter.
