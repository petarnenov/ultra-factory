# ao remote-worker

Inspect and reassign ownership of workers that run on linked remote AO hosts.
A link is identified by its remote host ID and the remote session ID; its owner
is a local coordinator, or `root` when no coordinator owns it.

The local daemon pulls a linked worker's reports from its host. For a host
found on the Tailscale network (see `ao host ls`) it does so at the host's
Tailscale address with this machine's Tailscale identity and no password; for
a host paired by address it uses the saved password. Either way the host must
have remote access on and be reachable.

For a legacy host whose mobile listener uses a nondefault port, run
`ao remote-host enable --also-tailnet` on that host once. It adds the
identity-only listener on the discovery port (default `3011`) while keeping
the existing mobile/LAN listener. If it already uses port `3011`, AO reuses
it and requires Tailscale identity only for tailnet requests. No extra
listener starts automatically.

## Syntax

```
ao remote-worker <subcommand> [args] [flags]
```

## Subcommands

### ao remote-worker ls

List linked remote workers with their local project, owner, and display name.

**Syntax:**
```
ao remote-worker ls [flags]
```

**Flags:**

| Flag | Meaning | Default / Required |
|---|---|---|
| `-p, --project string` | Only links owned by this local project | - |
| `--json` | Output as JSON | - |

**Examples:**

```bash
ao remote-worker ls
```

```bash
ao remote-worker ls --project mercury
```

---

### ao remote-worker assign

Assign, reassign, or clear a linked remote worker's local coordinator owner.
Only the project's live root may write ownership. Use `--owner root` to clear
the owner and let the link resolve to the project root.

**Syntax:**
```
ao remote-worker assign <host-id> <remote-session-id> --owner <coordinator-id|root> [flags]
```

**Flags:**

| Flag | Meaning | Default / Required |
|---|---|---|
| `--owner string` | Coordinator session id, or `root` to clear the owner | Required |
| `--requested-by string` | Session id of the assigning live root | `AO_SESSION_ID` |
| `--json` | Output as JSON | - |

The same ADR 0010 error codes apply as for local workers. A missing link returns
`REMOTE_WORKER_LINK_NOT_FOUND`.

**Examples:**

```bash
ao remote-worker assign build-box widgets-3 --owner mer-2
```

```bash
ao remote-worker assign build-box widgets-3 --owner root
```
