# ao host

List the remote AO hosts this machine can delegate work to with `--host`.

A remote host is another machine running AO that the user paired in the desktop
app under **Settings → Remote hosts**. Workers spawned there run in that
machine's worktree, with its agents, credentials, and build tools, and are
supervised by its daemon. The desktop shows them alongside local sessions.

## Syntax

```
ao host ls [--json]
```

`ao host ls` checks each saved host's identity before using its credential and
reports one status:

| Status | Meaning |
|---|---|
| `online` | Identity verified and the saved password accepted. Usable with `--host`. |
| `unauthorized` | The host rotated its password. Ask the user to update it in the desktop app. |
| `offline` | Unreachable, or not an AO daemon. |
| `identity-changed` | A different AO installation answers at that address. Do not use it. Ask the user to re-pair. |
| `needs-pairing` | Saved before identities were recorded. Ask the user to re-pair. |
| `this-machine` | The entry points at this machine. Omit `--host` instead. |

Passwords are never printed.

## Commands that accept `--host`

`--host <label | host ID | URL>` runs the command against that host's daemon:

- `ao spawn`
- `ao send`
- `ao session ls | get | kill | restore | rename | exit-agent | resume-agent`
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
