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

## Delegating through coordinators

When coordinators are enabled and an assignment has two or more bounded areas
that can run independently, spawn one coordinator per area instead of spawning
every worker yourself:

```bash
ao spawn --kind coordinator --scope <area> --max-workers <n> --hosts <label>:<slots>[,...] --prompt <brief>
```

The brief follows the template in `docs/coordinators/operating-contract.md`.
Check coordinators with `ao coordinator ls`, change limits with
`ao coordinator set-limits <coordinator-id>`, and move a worker with
`ao session assign <worker> --owner <coordinator-id>`. Only the root merges; a
coordinator reports `ao coordinator report --ready-pr <number>@<head-sha>`.
Keep one-off tasks as direct workers.

Host allocation: discover hosts with `ao host ls`, skip hosts that run their
own orchestrator or coordinator (`LOCAL_ORCHESTRATOR_ACTIVE`), probe each for
toolchain and required secrets with a short worker, then split slots as
`--hosts <label>:<slots>`. Never hard-code a host label in a prompt, brief or
worker prompt.

## Examples

```bash
# List all orchestrator sessions
ao orchestrator ls
```

```bash
# List orchestrator sessions as JSON
ao orchestrator ls --json
```
