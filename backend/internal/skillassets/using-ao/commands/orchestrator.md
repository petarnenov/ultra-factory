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

## Delegating to other hosts

Discover available hosts with `ao host ls`, start remote work with
`ao spawn --host <host> ...`, and follow it with
`ao session ls --host <host>`. See [host.md](host.md).

## Delegating through coordinators

When coordinators are enabled and an assignment has two or more bounded areas
that can run independently, delegate those areas to coordinators instead of
spawning every implementation worker yourself. Give each coordinator a full
brief with scope, exclusions, tasks and acceptance criteria, worker cap and
branch rules, and reporting cadence. Keep one-off tasks as direct workers.

```bash
ao spawn --kind coordinator --scope "<area>" --max-workers <n> \
  --hosts <label>:<slots>[,...] --prompt "<full brief>"
```

Use `ao coordinator ls` to inspect coordinator scopes and their local workers;
use `ao remote-worker ls` for linked remote workers. Change limits with
`ao coordinator set-limits <coordinator-id>`. Assign an existing local worker
with `ao session assign <worker> --owner <coordinator-id>` or a linked worker
with `ao remote-worker assign <host-id> <worker-id> --owner <coordinator-id>`.
Only the root merges. A coordinator reports ready PRs with
`ao coordinator report --ready-pr <number>@<head-sha>`.

### Host allocation

Discover candidate hosts with `ao host ls`. Check each with
`ao orchestrator ls --host <host>` and `ao coordinator ls --host <host>`; do not
allocate workers to a host running its own orchestrator or coordinator
(`LOCAL_ORCHESTRATOR_ACTIVE`). Probe candidates with a short worker for the
required toolchain and credentials, then allocate capacity per coordinator
with `--hosts <label>:<slots>`. Never hard-code a host label in a prompt, brief,
or worker prompt; resolve it from the host registry.

## Examples

```bash
# List all orchestrator sessions
ao orchestrator ls
```

```bash
# List orchestrator sessions as JSON
ao orchestrator ls --json
```
