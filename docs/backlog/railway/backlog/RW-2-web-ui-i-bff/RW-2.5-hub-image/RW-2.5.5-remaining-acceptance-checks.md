# RW-2.5.5 · Task: Remaining RW-2 acceptance checks: PR status, Electron-only sweep, image CSP decision
**Status:** ⛔ needs-human

**Type:** operations + validation. **Estimate:** 0.5 pd of operator time. **Story:** RW-2.5.

Follow-up from RW-2.5.3 (#104): the RW-2 epic (#18) acceptance criteria that were not fully verified on the live hub. #18 is closed with these items tracked here.

## Context
- Evidence so far: the consolidated tables in #104 (parts 1–11 and the final summary).

## Subtasks
- [ ] PR status in the browser (AC1): a `worker-1` session that opens a test PR shows its status on the board and in the session view.
- [ ] Electron-only sweep (AC3): walk the main screens of the web build on the hub and confirm desktop-only features are hidden and nothing breaks (#127 hid Codex accounts and enabled remote hosts; no full sweep was done).
- [ ] CSP decision (AC2): the SPA policy now has `connect-src 'self'` (#131) but keeps `img-src 'self' data: https:` for repository avatars and chat images. Decide: keep and amend the criterion, or add a same-origin image proxy.
- [ ] `make check` (AC4) green on CI for the current `dev` head, and for the `main` commit the hub image is deployed from (`hub-image.yml` publishes and deploys only on a push to `main`).

## Acceptance criteria
- [ ] Each of the four checks is answered in RW-2.5.5 (#142) with the result and, where a criterion changed, the reason.
- [ ] The CSP decision is recorded in the runbook or the frontend docs, so RW-2.1.2 does not read as an open question.
