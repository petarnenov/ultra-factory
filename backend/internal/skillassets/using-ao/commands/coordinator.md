# ao coordinator

Inspect a project's coordinator hierarchy (the root, each coordinator with its
scope, and the local workers it owns) and, as a coordinator, send the root a
bounded status summary or escalation.

## Syntax

```
ao coordinator <subcommand> [args] [flags]
```

## Subcommands

### ao coordinator ls

List a project's coordinators and the local workers they own, plus the root and
its directly owned workers. Terminated coordinators and workers are hidden
unless `--include-terminated` is passed. A terminated coordinator that still
owns a live worker is always listed and marked `NEEDS RECOVERY`; each
coordinator shows its latest summary or escalation when it has sent one.

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

### ao coordinator report

Send the project root a summary of your scope, or escalate. A summary is not a
worker report: it links at most three workers you own and never replaces their
reports. The recipient is always the root. The caller defaults to
`AO_SESSION_ID`; only a live coordinator may submit. Reusing `--submission-key`
returns the original summary and sends nothing new.

A `blocked` summary with `--escalate` is an escalation: it is delivered at once
and interrupts an active root (at most once per three minutes per coordinator;
further escalations are still delivered, just without a new interrupt). Other
summaries are batched into one turn per coordinator.

```
ao coordinator report --state <planning|working|blocked|done> [flags]
```

| Flag | Description | Default |
| --- | --- | --- |
| `--state` | Summary state (required) | - |
| `--worker` | Worker session id covered (repeatable, at most 3) | - |
| `--result` | Result text, at most 2000 characters | - |
| `--escalate` | `overlapping_files`, `unclear_acceptance`, `worker_failed`, `scope_decision`, or `other`; requires `--state blocked` | - |
| `--note` | Escalation note, at most 500 characters; required for `other` | - |
| `--scope` | Summary scope | the coordinator's scope |
| `--submission-key` | Idempotency key | a fresh key |
| `--requested-by` | Coordinator session id | `AO_SESSION_ID` |
| `--json` | Output as JSON | `false` |

```bash
ao coordinator report --state working --worker mercury-4 --result "auth tests green, wiring next"
```

```bash
ao coordinator report --state blocked --escalate scope_decision --note "needs a schema owner"
```
