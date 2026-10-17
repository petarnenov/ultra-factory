# RW-5.4.2 · Task: Wake-up and reconcile loop inside the hub
**Status:** ✅ done (#136)

**Type:** operations. **Estimate:** 1 pd. **Story:** RW-5.4.

## Goal
A hub orchestrator is woken periodically, like the Mac orchestrator is today, without launchd and without anything running on the operator's machines.

## Context
- `docs/backlog/railway/tools/orchestrator-tick.sh` runs every 5 minutes from launchd on the Mac. It finds the single live orchestrator (`ao orchestrator ls`), detects state changes and violations, and sends a `TICK` message. It logs to `~/.ao/rw-tick/tick.log`.
- The daemon has recurring automations (`ao automation create|list|runs`) that could replace the external scheduler.
- Remote reports reach the hub orchestrator through the ADR 0005 pull, so the tick is only for PR and CI state, which no report covers.

## Subtasks
- [ ] Choose the mechanism: an `ao automation` that runs the tick logic, or a supervised loop in `hub-supervisor.sh`. Prefer the automation if it can run a script with the orchestrator as target.
- [ ] Make `orchestrator-tick.sh` portable to the hub: no macOS-only commands, its state and log under `$AO_DATA_DIR`, and `gh` authenticated through `GH_TOKEN`.
- [ ] Keep the single-orchestrator check. Zero or several live orchestrators only log `VIOLATION`.
- [ ] Document how to pause the loop (a maintenance flag) for redeploys.

## Acceptance criteria
- [ ] With a hub orchestrator running, a green mergeable PR to the integration branch triggers a `TICK` within 10 minutes, without the Mac.

## Verification
- `bash docs/backlog/railway/tools/backlog-status-test.sh` (unchanged), plus a dry-run mode of the tick against fixture data.

## Dependencies
RW-5.4.1.
