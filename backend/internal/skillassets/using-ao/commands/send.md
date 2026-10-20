# ao send

Send a message to a running agent session. Use this to correct or direct a live agent mid-stream without killing and respawning it.

## Syntax

```
ao send [flags]
```

## Flags

| Flag | Meaning | Default / Required |
|---|---|---|
| `--message string` | Message body | Required unless `--recover-only` |
| `--session string` | Session id | Required |
| `--host string` | Send to a session on a paired remote AO machine. See [host.md](host.md) | This machine |
| `--steer` | Add guidance to the active Chat turn, or start a normal turn when idle | Optional |
| `--client-message-id string` | Stable delivery handle for steering and safe retries | Generated automatically with `--steer` |
| `--recover-only` | Recover the outcome for an existing delivery handle without resending | Requires `--steer` and `--client-message-id` |

## Steering and recovery

`--steer` uses one atomic daemon operation. If the session has an active Chat
turn, the message is added to that turn. If the session is idle, the message
starts a normal Chat turn. The state cannot change between those decisions and
leave the message queued as a separate correction.

Every steering request has a client message id. Supply one explicitly when a
caller must retain the handle across retries:

```bash
ao send --session mer-3 --steer --client-message-id correction-42 --message "Focus only on the backend."
```

If the command reports an uncertain outcome or a transport failure, preserve
the delivery handle printed in the error. Recover the existing result without
contacting the provider again:

```bash
ao send --session mer-3 --steer --recover-only --client-message-id correction-42
```

Do not invent a new client message id after an uncertain result. Reusing the
original id lets the daemon return the durable steering receipt or normal turn
without delivering the message twice. Recovery confirms the recorded delivery
outcome, not that the agent acted on the guidance.

## Pending structured questions

A session parked on an agent question (`health.inFlightTool: "question"`,
status `needs_input`) is waiting for a structured answer, not a chat message.
A normal `ao send` queues behind that turn, and `--steer` fails with
`CHAT_STEER_UNSUPPORTED` on harnesses that cannot take mid-turn guidance,
including `opencode-v2`. Do not retry steering. List the request and answer it
with [session input](session.md#ao-session-input):

```bash
ao session input mer-3
ao session input mer-3 --request-id 'acp-request:host:1' \
  --action accept --answer q0="No, leave it for the orchestrator"
```

The command also works with `--host` against a worker on another machine.

## Examples

```bash
# Send a correction to a running session
ao send --session mer-3 --message "Focus only on the backend; ignore frontend files."
```

```bash
# Give the agent new instructions mid-task
ao send --session mer-3 --message "The issue is in session_manager.go line 142, not in the CLI. Investigate there."
```

```bash
# Steer the active Chat turn, or start a normal turn if it is idle
ao send --session mer-3 --steer --message "Use the daemon DTO as the source of truth."
```
