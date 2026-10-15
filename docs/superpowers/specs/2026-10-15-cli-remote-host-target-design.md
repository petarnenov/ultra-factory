# CLI remote host target (`--host`) design

## Intent and scope

Let one orchestrator delegate workers to other machines. An orchestrator (or a
person) on machine A runs `ao spawn --host <name> ...` and the worker runs on
machine B, in B's worktree, supervised by B's daemon. The same orchestrator then
follows and steers that worker with `ao session ls|get --host <name>` and
`ao send --host <name>`. `ao host ls` lists the saved hosts it can target.

Out of scope for this change: a cross-host parent/child link in either daemon's
store, pushing remote session events into the local daemon, and making every
CLI command host-aware. The desktop already renders paired hosts' sessions
(#5921), so the board shows remote workers without new daemon state.

## Why the CLI, and why no new network surface

Every product command is a thin HTTP client. Machine B already serves its app
API on the opt-in authenticated remote listener (LAN, Cloudflare tunnel, or
Tailscale), and desktop A already holds B's credential and identity in
`remotes.json`. Pointing the CLI's existing request path at that listener gives
the orchestrator remote workers without a daemon-to-daemon protocol, a new
listener, a new route, or a new credential. The hard rules in `AGENTS.md` stay
intact: the loopback listener is unchanged and unauthenticated as before, and
the remote listener's `authMiddleware`, lockout, LAN control-route block, and
`expectedHostGuard` all apply to CLI traffic exactly as they do to desktop and
mobile traffic. No ADR is needed because no route or bind changes.

Alternatives considered:

1. **CLI `--host` over the existing authenticated listener (chosen):** smallest
   change, reuses pairing, identity, and authorization end to end.
2. **Local daemon proxies to remote daemons:** would let the local store track
   remote children, but makes the loopback daemon a credential-holding network
   client and needs a new internal protocol. Worth revisiting only if remote
   lifecycle reactions must run locally.
3. **SSH to the host and run `ao` there:** works today without code, but needs
   shell access and a second credential, and bypasses the host-ID check.

## Credential and identity handling

- **File:** `remotes.json` is the desktop's store (`{ remotes: [{ label, url,
  password, hostId, accountUserId? }] }`). Agents run with `AO_DATA_DIR` set to
  the daemon's data directory (`~/.ao/data`), while the packaged desktop writes
  `~/.ao/remotes.json`. The CLI therefore resolves: `AO_REMOTES_FILE` if set;
  otherwise `~/.ao/remotes.json` when `AO_DATA_DIR` is unset or is the
  desktop's default daemon directory (`~/.ao/data`, or `~/.ao/dev/data` in
  dev); otherwise `$AO_DATA_DIR/remotes.json`, even when it is missing. An
  isolated lab install therefore never reads the user's real saved hosts.
- **Permissions:** a file readable by group or others is refused (mirrors
  `remotes-store.ts`; Windows exempt for the same reason).
- **Selection:** `--host` matches a saved `hostId` exactly, then `label`
  case-insensitively, then `url`. No match or more than one match is a usage
  error that names the saved labels; it never guesses.
- **Identity before credential:** an entry without `hostId` must be re-paired
  (same rule as desktop). Before any authenticated request the CLI calls the
  unauthenticated `GET /api/v1/identity`, requires `apiVersion == 1`, a
  non-reserved host ID equal to the saved one, and a host that is not this
  machine's own daemon. Only then is the password sent.
- **Every request** carries `Authorization: Bearer <password>` and
  `X-AO-Expected-Host-ID`, so an address reassigned after the probe is still
  refused by the host (`421 HOST_ID_MISMATCH`).
- **No redirects, no proxies:** a 3xx is an error, so the bearer never follows
  a redirect to another origin. Remote requests ignore `HTTP(S)_PROXY`
  (agent sandboxes set one) so an intermediary can neither answer the identity
  probe nor read the plaintext bearer; the desktop's Node fetch also connects
  directly. URLs with embedded credentials or non-HTTP(S) schemes are
  refused.
- Loopback-only calls (`/internal/telemetry/*`) always go to the local daemon
  and never to a remote host.

## Command behavior with `--host`

Supported on: `spawn`, `send`, `session ls|get|kill|restore|rename|exit-agent|resume-agent`,
`project ls|get`, `orchestrator ls`, `agent ls`. Other commands do not accept
the flag, so they cannot accidentally reach a remote host.

Local session context does not cross hosts. Session IDs are per daemon
(`ao-3` on A is unrelated to `ao-3` on B), so a remote request never forwards
`AO_SESSION_ID` as `parentSessionId` or `senderSessionId`. `ao send` keeps
human-readable attribution in the message text: `[from ao-3 on <hostname>]`.
A steering recovery hint repeats `--host`, because recovery must ask the same
daemon.

Project resolution for `spawn --host` never uses the local `AO_PROJECT_ID`,
`AO_SESSION_ID`, or working directory as a remote project ID. Without
`--project`, it reads the local context project's repository (best effort) and
selects the single remote project with the same normalized repository
(`host/owner/name`, case-insensitive, `.git` and scheme ignored). Zero or
several matches is a usage error that lists the remote projects. A numeric
`--claim-pr` never shells out to `gh` in a remote path; it needs the remote
project's recorded repository or a full PR URL.

## Testing

Table tests in `backend/internal/cli` cover file resolution, permission refusal,
selector matching and ambiguity, missing host ID, identity mismatch (password
never sent), self-targeting, redirect refusal, auth headers, 401 guidance,
telemetry staying local, remote spawn project matching, omitted parent/sender
IDs, and that unsupported commands reject `--host`. A live check runs two real
daemons on one machine with isolated data directories and drives the second
through its authenticated remote listener.
