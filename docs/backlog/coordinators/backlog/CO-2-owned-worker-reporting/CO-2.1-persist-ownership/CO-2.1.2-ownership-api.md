# CO-2.1.2 · Task: Expose ownership through daemon and CLI APIs

**Status:** ⬜ todo

**Type:** API and CLI. **Estimate:** 1 pd. **Story:** CO-2.1.

## Goal

Expose assignment, reassignment, and ownership reads through supported daemon
routes and thin CLI commands so coordinators and operators use the same source
of truth.

## Context

- Controller DTOs and routes live under
  `backend/internal/httpd/controllers/`.
- The CLI must use daemon HTTP client helpers and mirror DTOs locally.
- OpenAPI and frontend types are generated with `npm run api`.

## Subtasks

- [ ] Add request and response DTOs plus route registration for the operations
  approved by the ADR.
- [ ] Add thin CLI commands or flags for assigning, reassigning, and listing
  ownership.
- [ ] Preserve API error envelopes, request IDs, and usage-error exit codes.
- [ ] Regenerate OpenAPI and frontend TypeScript types.
- [ ] Add tests for validation, missing sessions, daemon errors, and successful
  round trips.

## Acceptance criteria

- [ ] The daemon API is the only write path for coordinator ownership.
- [ ] CLI output identifies root, coordinator, and worker ownership clearly.
- [ ] `openapi.yaml` and `frontend/src/api/schema.ts` match the source DTOs.
- [ ] The CLI remains a thin HTTP client and does not access SQLite directly.

## Verification

- `npm run api`.
- `cd backend && go test ./internal/httpd/... ./internal/cli/...`.
- `npm run frontend:typecheck` when generated frontend API types change.

## Dependencies

CO-2.1.1.
