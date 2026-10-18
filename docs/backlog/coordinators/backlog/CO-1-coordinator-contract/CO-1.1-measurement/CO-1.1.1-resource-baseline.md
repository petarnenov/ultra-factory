# CO-1.1.1 · Task: Record per-process resource baseline

**Status:** ⬜ todo

**Type:** operations and measurement. **Estimate:** 0.5–1 pd. **Story:** CO-1.1.

## Goal

Produce a repeatable baseline for the existing root orchestrator with six
workers, before adding helper coordinators.

## Context

- Railway reported a 23.84 GB service-level memory maximum against a 24 GB
  limit during the observed 24-hour window. This does not identify the process
  responsible.
- The hub runs the daemon, BFF, optional orchestrator tick loop, and agent
  sessions. Agent harnesses may start additional child processes.
- Relevant deployment and runtime instructions are in
  `deploy/railway/README.md` and `docs/railway/architecture.md`.

## Subtasks

- [ ] Define a repeatable test workload with six concurrent workers and record
  the start/end times and worker harnesses.
- [ ] Capture timestamped CPU and RSS for the daemon, BFF, orchestrator,
  workers, and child processes where available.
- [ ] Capture corresponding Railway service CPU and memory samples.
- [ ] Summarize average, peak, duration near the memory limit, and any missing
  process attribution.

## Acceptance criteria

- [ ] The report links process-level samples to the same observation window as
  Railway service metrics.
- [ ] The report distinguishes measured facts from inference and names any
  process that could not be measured.
- [ ] No production deployment or configuration change is required to collect
  the baseline.

## Verification

- Review the sample timestamps and confirm the six-worker workload was active
  during the reported peaks.
- Repeat the capture once and document material variance.

## Dependencies

None.
