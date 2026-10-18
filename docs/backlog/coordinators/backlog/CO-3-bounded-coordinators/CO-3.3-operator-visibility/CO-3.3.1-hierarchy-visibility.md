# CO-3.3.1 · Task: Show coordinator ownership in session listings

**Status:** ⬜ todo

**Type:** API, CLI, and UI. **Estimate:** 1 pd. **Story:** CO-3.3.

## Goal

Make the coordinator tree and ownership state visible to operators using the
CLI and the existing session supervisor UI.

## Context

- Session data is exposed through daemon controllers and generated frontend
  API types.
- The frontend is a supervisor surface; orchestration rules remain in the
  daemon.
- Prefer extending existing session details and list views over adding a new
  control plane.

## Subtasks

- [ ] Expose root ID, coordinator ID, scope, and latest summary in session
  responses where appropriate.
- [ ] Add CLI output for the ownership tree, including JSON output.
- [ ] Render coordinator grouping and worker ownership in the existing
  session UI.
- [ ] Regenerate API artifacts and add focused API/renderer tests.

## Acceptance criteria

- [ ] Operators can identify unassigned, root-owned, and coordinator-owned
  workers.
- [ ] A terminated coordinator with live workers is visibly flagged for
  recovery.
- [ ] No orchestration decisions or direct storage access move into the
  frontend.

## Verification

- `npm run api` when response DTOs change.
- `cd backend && go test ./internal/httpd/... ./internal/cli/...`.
- `npm run frontend:typecheck` and focused renderer tests for changed views.

## Dependencies

CO-2.1.2, CO-2.2.2.
