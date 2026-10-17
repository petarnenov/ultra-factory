# RW-5.4.3 · Task: Cutover from the Mac orchestrator and end-to-end check
**Status:** ⬜ todo

**Type:** operations + validation. **Estimate:** 0.5–1 pd. **Story:** RW-5.4.

## Goal
Move coordination from the Mac orchestrator to a hub orchestrator without losing reports, and verify the whole path on every machine.

## Context
- Reports go to the machine that spawned the worker: `remote_worker_links` live in the spawner's database (ADR 0005). Workers started by the Mac keep reporting to the Mac until they end.
- `87c21b512` only stops a machine from both running workers for another orchestrator and running its own orchestrator. It does not stop two orchestrators on two machines from coordinating the same project. Retiring the Mac orchestrator is a deliberate step.
- The LAN machines' listeners: Linux and Z600 on `:3011`, IMac on its configured port. They must be reachable on the tailnet address (bind on all interfaces, or on the Tailscale interface).

## Subtasks
- [ ] Install Tailscale on IMac, Linux and Z600 and join them to the tailnet with the ACL from the ADR. Verify each listener answers `/api/v1/identity` from the hub over the tailnet. Change no router or firewall rule.
- [ ] Add the three machines in the hub UI by tailnet address (`http://100.x.y.z:<port>` or MagicDNS). Confirm `ao host ls` inside the hub (RW-5.3.1).
- [ ] Drain the Mac orchestrator: no new workers; wait until its remote workers end and their links are dropped (`ao session ls --host …`, the `remote reports: link removed` log).
- [ ] Stop the Mac orchestrator and unload its launchd tick. Start the hub orchestrator with the orchestrator prompt, and enable the hub wake-up loop (RW-5.4.2).
- [ ] End to end on each machine: spawn a worker, receive its `ao report` in the hub orchestrator exactly once, open a PR, and merge it into the integration branch after review.
- [ ] Update `deploy/railway/README.md` and `orchestrator-prompt.md` with the cutover and rollback (restart the Mac orchestrator, reload launchd).

## Acceptance criteria
- [ ] Hub orchestrator, three machines, one merged PR per machine, reports delivered exactly once, no inbound port at home.
- [ ] A rollback to the Mac orchestrator is documented and was tried once.

## Dependencies
RW-5.2, RW-5.3.1, RW-5.4.1, RW-5.4.2. `needs-human`: Tailscale enrollment on the machines and the cutover decision.
