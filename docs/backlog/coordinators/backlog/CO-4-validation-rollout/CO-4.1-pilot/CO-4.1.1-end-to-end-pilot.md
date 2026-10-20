# CO-4.1.1 · Task: Run coordinator hierarchy end-to-end

**Status:** ⛔ needs-human — the end-to-end test landed in [#339](https://github.com/petarnenov/ultra-factory/pull/339); the live pilot needs an operator and issue [#227](https://github.com/petarnenov/ultra-factory/issues/227) stays open.

**Type:** integration and operations. **Estimate:** 1–2 pd. **Story:** CO-4.1.

## Goal

Verify the root → two coordinators → six workers workflow, including report
routing, failure recovery, and resource use.

## Context

- This task validates the behavior implemented by CO-2 and CO-3.
- Use an isolated project and test environment; do not use production deployment
  as a validation step.
- Compare against the six-worker baseline from CO-1.1.1.

## Subtasks

- [ ] Start one root and two coordinators with disjoint scopes.
- [ ] Start three workers under each coordinator and verify capacity rejection
  for an additional worker.
- [ ] Verify each local and remote report reaches the assigned coordinator
  once and summaries reach the root.
- [ ] Restart a daemon during pending delivery and verify retry without loss or
  duplicate work.
- [ ] Terminate one coordinator and reassign its workers through the supported
  recovery flow.
- [ ] Verify only the root can merge and that the operator view shows all
  ownership and failure states.
- [ ] Capture per-process CPU/RSS and service metrics throughout the run.

## Acceptance criteria

- [ ] All six workers complete or report a defined blocker through their
  coordinator, and the root receives a complete summary.
- [ ] No report is lost or processed twice across restart and retry.
- [ ] Capacity and root-only integration rules hold under concurrent activity.
- [ ] The resource comparison states whether root load changed and identifies
  any remaining hub-wide bottleneck.

## Verification

- Run the repository CI checks appropriate to changed backend, API, and
  frontend areas, plus the isolated end-to-end scenario.
- Attach sanitized test logs and timestamped resource samples to the task
  record; include no credentials.

## Dependencies

CO-3.1.1, CO-3.2.1, CO-3.3.1.
