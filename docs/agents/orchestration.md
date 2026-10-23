# Orchestration and machine roles

Loaded on demand from [`AGENTS.md`](../../AGENTS.md). Read it before spawning workers, running reviews, or choosing a machine.

The user agreed this process on 2026-10-19 and moved the live orchestrator back to the Mac on 2026-10-20. It decides where work may run.

- **The Mac runs only the orchestrator.** The single live orchestrator runs in the desktop app on the user's Mac (the rollback in `deploy/railway/README.md`, "Rollback to the Mac orchestrator"). Do not spawn workers or reviews on that machine; it orchestrates and merges, and work goes to the worker machines.
- **The Railway hub is stopped.** Its orchestrator is stopped, so do not spawn from it or copy its configuration. Two live orchestrators on one backlog spawn duplicate workers and race on merges; check that exactly one is active before allowing new work.
- **Work runs on the worker machines.** Start implementation, fixes, tests and reviews on a paired worker machine (`Linux`, `Z600`, `IMac`) with `ao spawn --host`. A machine that runs its own orchestrator refuses a worker started from another machine with `LOCAL_ORCHESTRATOR_ACTIVE`, even when that local orchestrator belongs to another project, because it would claim the worker's reports. Observed on `IMac` with the local orchestrator `geowealth-1`. Check `ao orchestrator ls --host <H>` before starting a worker; stop that orchestrator or use another machine.
- **Workers report to their orchestrator.** A worker sends reports to the orchestrator that spawned it. The orchestrator does not wait for a report before inspecting state.
- **Workers cannot merge.** The daemon refuses `ao pr merge` from every live worker (and coordinator) with `403 MERGE_ROOT_ONLY`, in every project and whether or not coordinators are enabled. Integration belongs to the project root: report a green PR to your orchestrator and let it merge.
- **Reviews run where the worker's checkout lives.** The reviewer runs over that checkout with an enforced read-only guarantee. `ao review trigger|ls|cancel --host` runs them on the owning machine from another orchestrator. Before a pass: the worker is idle or exited, everything is committed and pushed, the PR is open, and the reviewer is given the evaluated head SHA. Findings go to the owning worker, which fixes and pushes; a moved head needs a new pass.
- **Agent configuration is per machine.** Project Settings / Agents and the machine's harness defaults apply on that machine. Precedence is defined in #254; do not copy the orchestrator machine's configuration to a worker.

Runbook: `deploy/railway/README.md` ("Orchestration roles and the review flow"). Orchestrator instructions: `docs/backlog/railway/orchestrator-prompt.md` ("Machine roles and the orchestration process").
