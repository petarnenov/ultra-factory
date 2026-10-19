# ao stop

> **Operator-only.** `ao stop` is refused inside an AO agent session
> (`AO_SESSION_ID` is set), and a daemon started by a host supervisor such as
> the Railway hub refuses the loopback shutdown request. Ask the operator to
> restart the hosted service instead.

Stop the AO daemon from a shell outside AO.

## Syntax

```
ao stop [flags]
```

## Flags

| Flag | Meaning | Default / Required |
|---|---|---|
| `--json` | Output stop result as JSON | - |
| `--timeout duration` | How long to wait for daemon shutdown | `10s` |

## Examples

```bash
# Stop the daemon
ao stop
```

```bash
# Stop with a longer timeout
ao stop --timeout 30s
```
