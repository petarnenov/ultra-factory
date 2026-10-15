# ao orchestrator

Manage orchestrator sessions.

## Syntax

```
ao orchestrator <subcommand> [flags]
```

## Subcommands

---

### ao orchestrator ls

List orchestrator sessions. Aliases: `ls`, `list`.

**Syntax:**
```
ao orchestrator ls [flags]
```

**Flags:**

| Flag | Meaning | Default / Required |
|---|---|---|
| `--json` | Output as JSON | - |
| `--host string` | List orchestrators on a paired remote AO machine | This machine |

## Delegating to other machines

An orchestrator can spread workers across machines the user paired as remote
hosts: `ao host ls`, then `ao spawn --host <name> ...`, and follow them with
`ao session ls --host <name>`. See [host.md](host.md).

## Examples

```bash
# List all orchestrator sessions
ao orchestrator ls
```

```bash
# List orchestrator sessions as JSON
ao orchestrator ls --json
```
