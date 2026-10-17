# RW-4.3.1 · Task: Local compose and Playwright e2e
**Status:** 👀 in review (#133)

**Type:** tests. **Estimate:** 1 pd. **Story:** RW-4.3.

## Context
- Precedents: `cloud/compose.yaml`, `frontend/playwright.config.ts`, `test/e2e-pod/`, `cloud/test/Dockerfile.worker-smoke` (fake `claude`).

## Subtasks
- [ ] `deploy/railway/compose.e2e.yaml`: `hub` (image from RW-2.5.1), `worker-1` (worker image with a fake `claude` script as in worker-smoke), `FakeRailwayProvider` in the BFF (`AO_HUB_PROVIDER=fake`), which "creates" a worker by returning the address of `worker-1`.
- [ ] Playwright specs: login; adding a worker by address; creating a worker via the fake provider → online; opening `host/<id>`; cloning a small local git repo (fixture); starting a session with the fake agent; the terminal shows output; removal with confirmation.
- [ ] CI job `railway-e2e.yml` (Docker), run on PRs that touch `deploy/railway/**`, `packages/hub-bff/**`, `frontend/src/renderer/**`.

## Acceptance criteria
- [ ] The e2e passes locally and in CI in < 10 min.
