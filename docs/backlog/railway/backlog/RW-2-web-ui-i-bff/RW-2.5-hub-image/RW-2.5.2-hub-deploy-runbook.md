# RW-2.5.2 · Task: Hub deployment on Railway and runbook

**Type:** operations + documentation. **Estimate:** 0.5–1 pd. **Story:** RW-2.5.

## Subtasks
- [ ] Service `hub` from the image, volume at `/data`, variables from RW-2.5.1, public domain, target port = `$PORT`.
- [ ] Generate the operator password hash locally (`node scripts/hash-password.mjs`) and set it as a variable; never plaintext.
- [ ] Add the worker from RW-1.4 through the UI (Settings → Remote hosts) with `http://worker-1.railway.internal:3011` (if IPv6 private networking works) or its public URL.
- [ ] Runbook `deploy/railway/README.md`: a "Hub" section: deployment, rotation of the operator password and session secret, backup of `/data`, image update (when the hub has no active sessions), logs.
- [ ] Validation log: SPA load time, SSE latency, terminal.

## Acceptance criteria
- [ ] The RW-2 epic criteria are verified and recorded.
