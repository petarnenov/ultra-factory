# RW-2.5.4 · Task: Measure hub load time, SSE latency and terminal typing latency
**Status:** ⛔ needs-human

**Type:** operations + measurement. **Estimate:** 0.5 pd of operator time. **Story:** RW-2.5.

Follow-up from RW-2.5.3 (#104): the performance checks that were not measured during the live hub validation. They are marked pending in `deploy/railway/README.md` under **Validation record → Pending live checks**; the per-check results belong in the **Hub validation record (RW-2.5.3)** table above that list.

## Context
- Measured so far: login-page assets TTFB 0.25–0.31 s from the client to `us-west2`; SSE streams observed open; terminal output verified (#104).

## Subtasks
- [ ] Board load time: in a logged-in browser on `https://<hub-domain>`, DevTools → Network, reload the board with cache disabled and enabled; record `DOMContentLoaded` and `Load`, client location, and the image revision.
- [ ] SSE delivery latency: two tabs on the board, make a visible change in one, measure until the other updates (or use EventStream timestamps). Record the method.
- [ ] Terminal typing latency on a `worker-1` session terminal (keystroke to echo), subjective rating plus a rough measurement if possible.
- [ ] Update the runbook's validation record rows (masked as in RW-1.4.1) and post the results in RW-2.5.4 (#141).

## Acceptance criteria
- [ ] The three measurements are in the runbook's validation record, masked as in RW-1.4.1, each with its method, the client location, and the image revision it was taken against.
- [ ] No production configuration change is required to collect them.
