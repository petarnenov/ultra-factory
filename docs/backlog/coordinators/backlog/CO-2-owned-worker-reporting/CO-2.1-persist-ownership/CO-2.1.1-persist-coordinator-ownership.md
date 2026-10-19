# CO-2.1.1 · Task: Persist coordinator ownership

**Status:** ✅ done — merged in [#296](https://github.com/petarnenov/ultra-factory/pull/296) (`a429aa38`); issue [#220](https://github.com/petarnenov/ultra-factory/issues/220) is closed.

**Type:** backend and storage. **Estimate:** 1–2 pd. **Story:** CO-2.1.

## Goal

Persist the owning coordinator for each worker, including remote workers, using
the ownership model approved in CO-1.2.1.

## Context

- Local session lifecycle and service boundaries are in
  `backend/internal/service/session/`.
- Remote worker ownership starts at
  `backend/internal/service/remoteworker/service.go` and the
  `remote_worker_links` table.
- SQLite changes require a new migration and source query changes followed by
  `npm run sqlc`; generated files must not be hand-edited.

## Subtasks

- [ ] Add the migration and storage queries needed by the ADR's ownership
  model.
- [ ] Validate that owner and worker belong to the same project and that the
  owner is a live coordinator session.
- [ ] Define migration behavior for existing local and remote workers.
- [ ] Make assignment and reassignment atomic and idempotent.
- [ ] Regenerate sqlc code and cover migration, validation, and restart reads.

## Acceptance criteria

- [ ] Ownership survives daemon restart and is readable from the store.
- [ ] Invalid cross-project, terminated, or non-coordinator owners are
  rejected with stable API errors.
- [ ] Existing workers retain their current root-routing behavior after
  migration.
- [ ] The migration is additive and does not modify an already-merged
  migration.

## Verification

- Focused storage and service tests, including migration from a database with
  existing local and remote worker records.
- `npm run sqlc` and `cd backend && go test ./internal/storage/sqlite/... ./internal/service/remoteworker/...`.

## Dependencies

CO-1.2.1.
