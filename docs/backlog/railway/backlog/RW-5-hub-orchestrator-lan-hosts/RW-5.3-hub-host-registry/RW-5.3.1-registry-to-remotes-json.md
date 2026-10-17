# RW-5.3.1 · Task: Mirror the hub host registry into the daemon's `remotes.json`
**Status:** ⬜ todo

**Type:** BFF + infrastructure. **Estimate:** 1 pd. **Story:** RW-5.3.

## Goal
The hub's BFF registry (`/data/hub/hosts.json`) stays the single source of truth. The daemon, and every agent in the hub, sees the same verified hosts through the `remotes.json` file that `ao spawn --host`, `ao host ls` and the report puller already read.

## Context
- `packages/hub-bff/src/hosts-store.ts`: `HostEntry` (`label`, `url`, `password`, `hostId`, `accountUserId`, plus hub bookkeeping: `source`, `status`, `railway`, …). The `hub` entry (`source: "hub"`) is the hub's own daemon on loopback.
- `backend/internal/remotehost/remotehost.go`: `Entry` (`label`, `url`, `password`, `hostId`, `accountUserId`) inside `{"remotes": [...]}`. `FilePath()` honors `AO_REMOTES_FILE`, then `AO_DATA_DIR`. AO only reads this file.
- In the hub, `AO_DATA_DIR=/data/ao` (supervisor), so today the daemon would look for `/data/ao/remotes.json`, which does not exist.

## Subtasks
- [ ] After every registry write (add, remove, identity confirmed, Railway worker reconcile), have the BFF atomically write `{"remotes": [...]}` to a mirror file (`/data/hub/remotes.json`, mode `0600`, uid 10001). Include only entries with a confirmed `hostId` and `desired !== "removed"`. Exclude the `hub` self entry.
- [ ] The supervisor exports `AO_REMOTES_FILE=/data/hub/remotes.json` to the daemon, so agents spawned by the daemon inherit it.
- [ ] Labels must be unique and usable as `--host` selectors. Reject or suffix duplicates in the BFF.
- [ ] Tests: the mirror content follows add, remove and identity changes; file mode; the self entry is excluded; no password ever appears in BFF responses or logs.

## Acceptance criteria
- [ ] Inside the hub, `ao host ls` matches the hub UI's host list, minus the `hub` entry.
- [ ] `ao spawn --host <label>` from an orchestrator in the hub targets the machine added in the UI.

## Verification
- `npm --prefix packages/hub-bff test`, `bash deploy/railway/hub/supervisor_test.sh`. Live: add a host in the UI, then `railway ssh --service hub -- ao host ls`.

## Dependencies
RW-5.1.1.
