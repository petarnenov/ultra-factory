# RW-4.2.2 · Task: Limits and cost monitoring

**Type:** operations + Node TS. **Estimate:** 0.5 pd. **Story:** RW-4.2.

## Subtasks
- [ ] `AO_HUB_MAX_WORKERS` is already in RW-3.2.2; add `AO_HUB_MAX_VOLUME_GB` for spec validation.
- [ ] Idle worker: if 0 sessions for > N hours (`AO_HUB_IDLE_STOP_HOURS`, default off) → UI warning; automatic stop only if the provider supports stop/start without losing the volume (check in RW-3.1.1).
- [ ] Runbook: how to read Railway usage for the project; recommended sizes.
