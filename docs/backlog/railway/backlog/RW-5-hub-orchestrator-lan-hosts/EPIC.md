# RW-5 · Epic: Hub orchestrator managing machines outside Railway
**Status:** ⬜ todo

**Goal.** The orchestrator runs in the Railway hub and delegates workers to the operator's own machines (for example IMac, Linux, Z600 on a home LAN), not only to Railway workers. The hub reaches those machines over an encrypted private path, without opening inbound ports at home, and the desktop orchestrator on the Mac is no longer required.

**Estimate:** 6–9 pd. **Stage:** after the full goal. **Dependencies:** RW-2 (hub, BFF, host registry), RW-4.1 (threat model).

| Story | Name | pd |
|---|---|---|
| RW-5.1 | ADR: network path from the hub to machines outside Railway | 0.5–1 |
| RW-5.2 | Tailnet egress from the hub (image, daemon, BFF) | 3–4 |
| RW-5.3 | One host list for the hub UI and `ao spawn --host` | 1 |
| RW-5.4 | Orchestrator in the hub: credentials, wake-up loop, cutover | 2–3 |

## Why the hub cannot do this today (code findings)
- **No route.** The LAN machines have private addresses behind NAT. Railway private networking only connects services inside the Railway project.
- **The daemon refuses proxies on purpose.** `remotehost.DirectClient` (`backend/internal/remotehost/remotehost.go`) clones the transport with `Proxy = nil`, so a saved bearer never crosses an `HTTP(S)_PROXY`. It is used by `ao spawn --host` (`cli/remote_target.go`) and by the report puller (`observe/remotereports/observer.go`). Railway containers have no `/dev/net/tun`, so Tailscale can only run in userspace mode, which exposes the tailnet through a local SOCKS5/HTTP proxy, not a route.
- **The BFF dials directly.** `packages/hub-bff/src/hosts-probe.ts` and `workers-api.ts` use `fetch`, and `proxy.ts` uses `node:http`/`node:https` (REST, SSE and the `/mux` WebSocket upgrade). None of them can reach a userspace tailnet today.
- **Two host lists.** The hub UI writes `/data/hub/hosts.json` (`hosts-store.ts`). `ao spawn --host`, `ao host ls` and the report puller read the desktop's `remotes.json` (`remotehost.FilePath`). A host added in the hub UI is invisible to an orchestrator running in the hub.
- **Plain http is already acceptable over Tailscale.** `remotehost.UnattendedTransportAllowed` allows `http://` to `100.64.0.0/10` and `*.ts.net` without `AO_REMOTE_REPORTS_ALLOW_HTTP`, because WireGuard encrypts the traffic.
- **One orchestrator is an operational rule, not enforced across machines.** `refuseOrchestratorForRemoteWorkers` / `refuseRemoteWorkerForLocalOrchestrators` (`service/session/service.go`, from `87c21b512`) only stop a machine from both running workers for another orchestrator and running its own orchestrator. Nothing stops the Mac orchestrator and a hub orchestrator from coordinating the same project at once, so the cutover has to retire the Mac one deliberately.
- **The wake-up loop lives on the Mac.** `docs/backlog/railway/tools/orchestrator-tick.sh` runs from launchd on the Mac, so a hub orchestrator needs its own wake-up.

**Acceptance criteria**
- [ ] From the hub web UI an operator adds IMac, Linux and Z600 by tailnet address. All three are online, and `ao host ls` inside the hub lists them.
- [ ] An orchestrator started in the hub spawns a worker on each machine with `ao spawn --host`. The workers' reports reach the hub orchestrator through the ADR 0005 pull, exactly once.
- [ ] The saved bearer never crosses a proxy other than the hub's own loopback tailnet proxy, and only toward tailnet destinations (covered by tests).
- [ ] No inbound port is opened on the home network. The LAN machines' listeners are reachable only over the tailnet.
- [ ] The Mac orchestrator and its launchd tick are retired with a documented cutover, without losing reports from workers it had started.
