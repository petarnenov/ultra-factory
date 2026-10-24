## 1. Preflight behavior and focused tests

- [x] 1.1 Add ordered Node-major and pinned-executable checks with actionable nonzero diagnostics.
- [x] 1.2 Add focused Node tests asserting exact setup diagnostics and that failure cases never spawn OpenSpec.
- [x] 1.3 Preserve and verify the successful sample, malformed-fixture, repaired-control, and pinned-version behavior.

## 2. Capability evidence

- [x] 2.1 Declare and strictly validate the repository-local `openspec-check-setup` capability for R647-01 through R647-03.
- [ ] 2.2 Manually compare the implementation and tests to the declaration; record final-head evidence in the PR.
- [x] 2.3 Synchronize and archive this change with the pinned CLI without `--skip-specs`; verify the spec and archive outputs.

## 3. Delivery

- [ ] 3.1 Run coordinator-scheduled applicable checks and address failures.
- [ ] 3.2 Open one draft PR to `dev` with `Closes #647`, link #633, and record proposal/ruling, branch, archive, and final-head evidence.
- [ ] 3.3 Obtain exact-head native review and hand off for root integration without merging or closing the issue.
