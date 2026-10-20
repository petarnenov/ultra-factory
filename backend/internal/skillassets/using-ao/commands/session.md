# ao session

Manage agent sessions: list, inspect, rename, kill, restore, exit or resume an
agent, switch harnesses, clean up, claim PRs, and answer a pending structured
question.

## In-app session links

When referring a user or another agent to a session in AO Chat or the AO
terminal, emit this canonical link:

```text
ao://sessions/{project-id}/{session-id}
```

- Use stable IDs returned by `ao project ls` and `ao session ls`, or the current
  session's `AO_PROJECT_ID` and `AO_SESSION_ID`. Never use display names as
  identity; renaming a session must not change its link.
- Treat the project ID and session ID as separate URL path segments. Encode
  either ID with URL percent encoding if it contains characters that are not
  safe in one segment.
- To link a file in a session's artifact directory for the human, append
  `/artifacts/{path}`, where `{path}` is relative to that directory and each
  segment is percent-encoded: `ao://sessions/{project-id}/{session-id}/artifacts/{path}`.
  Opening it navigates to the session and opens the artifact.
- Emit only these navigation routes. Do not append query strings, fragments,
  action names, credentials, authorities, or extra path segments.
- The link is handled only inside a currently running AO desktop app. It is not
  an operating-system protocol link and must not be described as supporting
  cold launch or navigation from external applications.
- A session link navigates; it never sends a message, executes a command, or
  performs another action.

Example for project `mercury` and session `mer-3`:

```text
ao://sessions/mercury/mer-3
```

## Syntax

```
ao session <subcommand> [args] [flags]
```

## Subcommands

---

### ao session ls

List sessions. The `OWNER` column names a worker's coordinator owner, or `root`
when the worker is root-owned. Coordinator and orchestrator rows show `-`
because only workers can be owned.

**Syntax:**
```
ao session ls [flags]
```

**Flags:**

| Flag | Meaning | Default / Required |
|---|---|---|
| `-a, --all` | Include orchestrator sessions | - |
| `--include-terminated` | Include terminated sessions | - |
| `--json` | Output as JSON | - |
| `-p, --project string` | Filter by project ID | - |
| `--tree` | Show the ownership tree: root, coordinators with scope and latest summary, and their workers | - |

`--tree` flags a terminated coordinator that still owns live workers with
`NEEDS RECOVERY`, and labels root-owned workers `unassigned` when the project
has no live root. With `--json` it prints `{"data": [<roster>, ...]}`.

**Examples:**

```bash
# List all active worker sessions
ao session ls
```

```bash
# Show who owns which worker
ao session ls --tree -p agent-orchestrator
```

```bash
# List all sessions including terminated, scoped to one project
ao session ls --include-terminated -p agent-orchestrator
```

---

### ao session top

Show resident memory (RSS) and process count of each live session's process
tree, largest first, with a `TOTAL` row. Unmeasured sessions print `-`, and
`--json` reports `rssBytes` and `processCount` as `null` rather than a zero
reading. It reports RSS and process counts only; CPU percent and the individual
process list are available from the daemon's `GET /api/v1/usage/sessions/memory`
route.

**Syntax:**
```
ao session top [flags]
```

**Flags:**

| Flag | Meaning | Default / Required |
|---|---|---|
| `-a, --all` | Include orchestrator sessions | - |
| `--json` | Output as JSON | - |
| `-p, --project string` | Filter by project ID | - |
| `--host string` | Run against a saved remote AO host instead of this machine | - |

**Examples:**

```bash
# Show the largest sessions on this machine
ao session top
```

```bash
# Sample a paired worker host's daemon in the same pass
ao session top --host build-box --all
```

---

### ao session get

Fetch one session. For a coordinator it also prints its workers, a
`needs recovery` line when it is terminated but still owns live workers, and
its latest summary; `--json` adds a `coordinator` object with the same facts.

**Syntax:**
```
ao session get <id> [flags]
```

**Flags:**

| Flag | Meaning | Default / Required |
|---|---|---|
| `--json` | Output as JSON | - |
| `-p, --project string` | Project id to scope the lookup | - |

**Examples:**

```bash
# Get details for session mer-3
ao session get mer-3
```

```bash
# Get session details as JSON
ao session get mer-3 --json
```

---

### ao session owner

Show one session's durable ownership relation: its kind, its coordinator owner
(if any), the owner's scope, and the project's live root.

**Syntax:**
```
ao session owner <id> [flags]
```

**Flags:**

| Flag | Meaning | Default / Required |
|---|---|---|
| `--json` | Output as JSON | - |
| `-p, --project string` | Project id to scope the lookup | - |
| `--host string` | Read through a saved remote host's daemon | this machine |

**Examples:**

```bash
ao session owner mer-3
```

```bash
ao session owner mer-3 --json
```

---

### ao session assign

Assign, reassign, or clear a worker's coordinator owner. Only the project's
live root may write ownership. Use `--owner root` to return a worker to direct
root delegation.

The daemon enforces ADR 0010: a root and a coordinator are never owned, the
owner must be a live coordinator in the worker's project, and an explicit
reassignment is refused while one of the worker's reports is claimed. Rejections
use the stable `OWNERSHIP_ROOT_ONLY`, `OWNER_NOT_COORDINATOR`,
`OWNER_CROSS_PROJECT`, `OWNER_TERMINATED`, and `OWNER_REASSIGNMENT_IN_FLIGHT`
codes with a request ID.

**Syntax:**
```
ao session assign <worker> --owner <coordinator-id|root> [flags]
```

**Flags:**

| Flag | Meaning | Default / Required |
|---|---|---|
| `--owner string` | Coordinator session id, or `root` to clear the owner | Required |
| `--requested-by string` | Session id of the assigning live root | `AO_SESSION_ID` |
| `--json` | Output as JSON | - |
| `-p, --project string` | Project id to scope the lookup | - |
| `--host string` | Write through a saved remote host's daemon | this machine |

With `--host`, always pass `--requested-by` explicitly: a local
`AO_SESSION_ID` is never forwarded to another daemon.

**Examples:**

```bash
# Give mer-3 to coordinator mer-2
ao session assign mer-3 --owner mer-2
```

```bash
# Return mer-3 to direct root ownership
ao session assign mer-3 --owner root
```

```bash
# Assign on a paired host as that host's root
ao session assign mer-3 --host build-box --requested-by build-root --owner mer-2
```

---

### ao session input

List or answer a session's pending structured question (an agent elicitation).
A chat-mode worker that asks a question parks its turn, so a normal message
queues behind it instead of reaching the agent.

Run the command without `--action` to list pending inputs. The output includes
the request id, the question, the form fields, and an `Answer with:` line you
can copy. Then resolve one request:

- `accept` answers a form, or records consent to open a URL;
- `decline` declines the request;
- `cancel` cancels it.

URL consent never opens the URL from the CLI. The user opens it in AO first.

**Syntax:**
```
ao session input <id> [flags]
```

**Flags:**

| Flag | Meaning | Default / Required |
|---|---|---|
| `--action string` | Resolve the input: `accept`, `decline`, or `cancel` | Required to resolve |
| `--answer string` | Structured answer as `key=value`; repeat once per field | Accept only |
| `--request-id string` | Pending input request id shown by the listing | Required with `--action` |
| `--json` | Output the pending list or resolution as JSON | - |
| `--host string` | Target a paired remote AO machine. See [host.md](host.md) | This machine |

`--answer` values that are valid JSON keep their type, so a boolean or number
schema gets `true` or `2`, not a string. Quote a string that would otherwise
parse as JSON. The daemon validates field names, required fields, and offered
values, so an invalid answer leaves the request pending and reports
`CHAT_DECISION_NOT_OFFERED`. An unknown or already answered request reports
`CHAT_REQUEST_NOT_PENDING`.

**Examples:**

```bash
# List what the worker is waiting on
ao session input mer-3
```

```bash
# Answer the single free-text field
ao session input mer-3 --request-id 'acp-request:host:1' \
  --action accept --answer q0="No, leave it for the orchestrator"
```

```bash
# Decline the request from another machine
ao session input mer-3 --host build-box \
  --request-id 'acp-request:host:1' --action decline
```

```bash
# Script-friendly pending list
ao session input mer-3 --json
```

The command works for harnesses whose Chat transport supports structured input
but not mid-turn steering, including `opencode-v2`; it calls the daemon's
resolve route directly instead of using `ao send --steer`.

---

### ao session kill

Terminate a session.

**Syntax:**
```
ao session kill <id> [flags]
```

**Flags:**

| Flag | Meaning | Default / Required |
|---|---|---|
| `-p, --project string` | Project id to scope the lookup | - |

**Examples:**

```bash
# Kill session mer-3
ao session kill mer-3
```

---

### ao session rename

Rename a worker session. The project orchestrator cannot be renamed, including by itself; the daemon refuses with `ORCHESTRATOR_RENAME_UNSUPPORTED`.

**Syntax:**
```
ao session rename <id> <name> [flags]
```

**Flags:**

| Flag | Meaning | Default / Required |
|---|---|---|
| `-p, --project string` | Project id to scope the lookup | - |

**Examples:**

```bash
# Rename session mer-3 to a new display name
ao session rename mer-3 "fix-auth-bug"
```

---

### ao session restore

Restore a terminated session or resume an exited agent.

**Syntax:**
```
ao session restore <id> [flags]
```

**Flags:**

| Flag | Meaning | Default / Required |
|---|---|---|
| `-p, --project string` | Project id to scope the lookup | - |

**Examples:**

```bash
# Restore a terminated session
ao session restore mer-3
```

---

### ao session exit-agent

Exit an agent without terminating its AO session. The session can later be
resumed in place.

**Syntax:**
```
ao session exit-agent <id> [flags]
```

**Flags:**

| Flag | Meaning | Default / Required |
|---|---|---|
| `--json` | Output as JSON | - |
| `-p, --project string` | Project id to scope the lookup | - |

---

### ao session resume-agent

Resume an exited agent in its existing AO session.

**Syntax:**
```
ao session resume-agent <id> [flags]
```

**Flags:**

| Flag | Meaning | Default / Required |
|---|---|---|
| `--json` | Output as JSON | - |
| `-p, --project string` | Project id to scope the lookup | - |

**Examples:**

```bash
ao session exit-agent mer-3
ao session resume-agent mer-3
```

---

### ao session switch-agent

Switch a running session to another installed agent harness. This preserves the
AO session while provisioning the target harness.

**Syntax:**
```
ao session switch-agent <id> <target-harness> [flags]
```

**Flags:**

| Flag | Meaning | Default / Required |
|---|---|---|
| `--idempotency-key string` | Reuse a prior identical switch request safely | - |
| `--json` | Output the agent switch as JSON | - |

**Examples:**

```bash
ao session switch-agent mer-3 codex
```

---

### ao session agent-switch ls

List agent-harness switches recorded for a session. Alias: `agent-switches`.

**Syntax:**
```
ao session agent-switch ls <session-id> [flags]
```

**Flags:**

| Flag | Meaning | Default / Required |
|---|---|---|
| `--json` | Output agent switches as JSON | - |

**Examples:**

```bash
ao session agent-switch ls mer-3
```

---

### ao session cleanup

Clean up terminated sessions by reclaiming eligible workspaces. Dirty worktrees are skipped by the daemon.

**Syntax:**
```
ao session cleanup [flags]
```

**Flags:**

| Flag | Meaning | Default / Required |
|---|---|---|
| `--dry-run` | Preview which sessions would be cleaned without removing anything | - |
| `-p, --project string` | Filter by project ID | - |
| `-y, --yes` | Skip confirmation prompt | - |

**Examples:**

```bash
# Clean up all terminated sessions (skip prompt)
ao session cleanup -y
```

```bash
# Clean up terminated sessions for one project
ao session cleanup -p agent-orchestrator
```

```bash
# Preview eligible cleanup candidates
ao session cleanup --dry-run
```

---

### ao session claim-pr

Attach an existing PR to the current AO session, or target another session explicitly.
Use this for authorized implementation continuation. Reviewing someone else's PR
does not require or permit a claim; use a normal review-only worker session.

**Syntax:**
```
ao session claim-pr <pr-ref> [flags]
ao session claim-pr <session-id> <pr-ref> [flags]
```

With one positional argument, `AO_SESSION_ID` supplies the session. This is the preferred form inside a worker. Pass both arguments from an orchestrator or external shell when targeting another session.

**Flags:**

| Flag | Meaning | Default / Required |
|---|---|---|
| `--json` | Output as JSON | - |
| `--no-takeover` | Refuse if another active session owns the PR | - |
| `-p, --project string` | Project id to scope the lookup | - |

**Examples:**

```bash
# Attach PR 88 to the current worker session
ao session claim-pr 88
```

```bash
# Attach PR 88 to session mer-3 explicitly
ao session claim-pr mer-3 88
```

```bash
# Claim PR 88 for the current worker but refuse if another session owns it
ao session claim-pr 88 --no-takeover
```
