# ao coordinator

Inspect a project's coordinator hierarchy: the root, each coordinator with its
scope, and the local workers it owns.

## Syntax

```
ao coordinator <subcommand> [args] [flags]
```

## Subcommands

### ao coordinator ls

List a project's coordinators and the local workers they own, plus the root and
its directly owned workers. Terminated coordinators and workers are hidden
unless `--include-terminated` is passed.

Linked remote workers are listed by `ao remote-worker ls`; their owner is the
same local coordinator ID.

**Syntax:**
```
ao coordinator ls [flags]
```

**Flags:**

| Flag | Meaning | Default / Required |
|---|---|---|
| `-p, --project string` | Project id | `AO_PROJECT_ID` |
| `--include-terminated` | Include terminated coordinators and workers | - |
| `--json` | Output as JSON | - |
| `--host string` | Read through a saved remote host's daemon | this machine |

A missing project ID is a usage error and exits 2.

**Examples:**

```bash
ao coordinator ls
```

```bash
ao coordinator ls --project mercury --json
```

```bash
# Show a coordinator's roster after a helper failed
ao coordinator ls --project mercury --include-terminated
```
