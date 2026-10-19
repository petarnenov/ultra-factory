# Session health and stall runbook

A session can say `activity_state=active` while producing nothing. The health
monitor exists to make that visible without polling and without mistaking a
long tool call for a hang.

## What is recorded

The daemon records compact progress heartbeats per session in
`session_progress`:

| Signal class | Producer |
| --- | --- |
| `provider` | Chat/ACP stream events (turn start/end, message deltas, reasoning, command output, plans) and TUI hook boundaries |
| `tool` | Tool-call start/end with the tool name, from hooks and Chat activity items |
| `terminal` | Changed PTY output, read by the existing activity observer |
| `git` | Branch-tip or tracked working-tree change in the session workspace |

Only durable facts are stored: the last timestamp per class, the in-flight tool
name, a durable blocked observation, and stall counters. The health state is
**derived at read time** and is never persisted. `activity_state` keeps its
existing meaning.

## Derived states

| State | Meaning |
| --- | --- |
| `ok` | Recent progress, an in-flight tool inside its budget, or the session is deliberately not working (idle, waiting, hibernated, exited). |
| `stalled` | The session reports `working`, its process is alive, and no progress signal arrived inside the budget for the freshest class. |
| `blocked` | The session is paused on input, or a durable auth/credential failure was observed, past the blocked budget. |
| `unreachable` | The owning host could not be reached. This is **not** a stalled agent. |
| `unknown` | Not enough durable evidence yet (terminated, provisioning, or no baseline). |

The `in-flight tool` rule is what separates a hang from legitimate long work:
a tool that has not ended is given its own budget, and a `stalled` decision is
withheld while that budget is still open.

## Where it is surfaced

- `ao session ls` prints a `HEALTH` column.
- `ao session ls --json` and `ao session get --json` include a `health` object:
  `state`, `reason`, `class`, `lastProgressAt`, `silentForSeconds`,
  `thresholdSeconds`, `inFlightTool`, `stallCount`, `lastStallAt`, and
  `lastStallForSeconds`.
- The desktop board shows a red `Stalled`/`Blocked` badge with the silence
  duration, and the notification center receives a `session_stalled`
  notification.
- On the first threshold crossing of a silent episode, the daemon creates a
  `stuck` report for the session. Reports use the existing report outbox, so an
  orchestrator on another machine receives it through the remote-report pull;
  no polling is required. The notification and report are resolved when real
  progress resumes, and not repeated for the same silent episode.

## Thresholds

Defaults are conservative and configurable in the daemon environment:

| Variable | Default | Why |
| --- | --- | --- |
| `AO_STALL_PROVIDER_AFTER_SECONDS` | `900` (15m) | A Chat/ACP stream normally emits turn, message, or tool events; 15m of total silence is abnormal but leaves room for slow model turns. |
| `AO_STALL_TOOL_AFTER_SECONDS` | `1800` (30m) | Covers ordinary long tool calls. |
| `AO_STALL_LONG_TOOL_AFTER_SECONDS` | `3600` (60m) | Covers full test suites and image builds for tools named in `AO_STALL_LONG_TOOLS`. |
| `AO_STALL_TERMINAL_AFTER_SECONDS` | `1200` (20m) | A PTY can be quiet during a build; tool boundaries are the primary signal. |
| `AO_STALL_GIT_AFTER_SECONDS` | `1200` (20m) | Git progress is corroborating evidence, not the primary work signal. |
| `AO_STALL_BLOCKED_AFTER_SECONDS` | `1200` (20m) | Gives an agent time to report `needs_input` on its own. |
| `AO_STALL_LONG_TOOLS` | `bash,shell,run_shell_command,run_command,execute,exec_command,terminal,task,agent,subagent,wait` | Tool names normalized to lower case. |
| `AO_STALL_TICK_SECONDS` | `30` | Observation cadence. |
| `AO_STALL_CHECKPOINT_REQUEST` | unset | Set to `1` to send the stalled session a checkpoint request. It is off by default. |

An invalid value fails daemon startup instead of silently disabling the
safeguard.

## Orchestrator response

The orchestrator must distinguish the three stall classes before acting:

1. **Hung agent (`stalled`).** Check the report and the last progress class. If
   the tool is not obviously long-running, ask the worker for a checkpoint or
   a status report. Actual recovery (interrupt, restart, kill) is deliberately
   not automated by this feature.
2. **Unreachable machine (`unreachable`, or no health answer).** Run
   `ao host ls`. An offline host is not a stalled agent: the daemon itself is
   unreachable and messages cannot be delivered. Do not kill or respawn the
   session; wait for the host to return.
3. **Blocked (`blocked`).** Read the reason. `waiting_on_input` means the agent
   is paused on a decision. `git_credential_missing` or
   `provider_auth_required` means the machine cannot authenticate; repair the
   credential on that machine before re-driving the session. A worker's local
   commits stay on that machine, so killing it loses work.

The counters in `health` are for tuning: a high `stallCount` with short
`lastStallForSeconds` values is evidence of false positives; a long
`lastStallForSeconds` is evidence of a real hang.
