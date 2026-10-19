# ao remote-worker

Inspect and reassign ownership of workers that run on linked remote AO hosts.
A link is identified by its remote host ID and the remote session ID; its owner
is a local coordinator, or `root` when no coordinator owns it.

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
