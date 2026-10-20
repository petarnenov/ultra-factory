# ao start

> **Operator-only.** `ao start` is refused inside an AO agent session
> (`AO_SESSION_ID` is set). An agent must not launch or restart the app.

Fetch (if needed) and open the Agent Orchestrator desktop app. The desktop app owns the daemon, state, and updates. `ao start` no longer runs a daemon: it resolves the installed app (or downloads the latest release), opens it, and exits.

## Syntax

```
ao start [flags]
```

## Flags

| Flag | Meaning | Default / Required |
|---|---|---|
| `--json` | Output start result as JSON | - |

## Examples

```bash
# Open the AO desktop app
ao start
```

```bash
# Open the app and get the result as JSON
ao start --json
```
