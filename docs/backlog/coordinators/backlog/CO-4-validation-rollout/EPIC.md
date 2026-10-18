# CO-4 · Epic: Validate and roll out hierarchical coordination

**Status:** ⬜ todo

**Goal.** Prove the two-coordinator, six-worker topology handles delivery,
recovery, operator visibility, and resource use before enabling it for regular
work.

**Estimate:** 2–4 pd. **Stage:** release. **Dependencies:** CO-3.

| Story | Name | Estimate |
|---|---|---:|
| CO-4.1 | End-to-end pilot and resource comparison | 1–2 pd |
| CO-4.2 | Rollout, rollback, and operator documentation | 1–2 pd |

**Acceptance criteria**

- [ ] The end-to-end scenario runs two coordinators with three workers each.
- [ ] Reports and summaries are delivered once through restart and retry
  scenarios.
- [ ] Coordinator failure and reassignment are exercised.
- [ ] Per-process resource measurements are compared to the CO-1 baseline.
- [ ] Rollout and rollback steps are documented and do not require hub
  replicas or a shared SQLite volume.
