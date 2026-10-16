# RW-4.2.1 · Task: Backup and redeploy strategy

**Type:** operations. **Estimate:** 0.5 pd. **Story:** RW-4.2.

## Context
- A container restart kills the PTY host/tmux processes (`spawn_unix.go`, `Setsid`); the DB and worktrees survive on the volume.

## Subtasks
- [ ] Script `deploy/railway/scripts/backup-volume.sh`: `railway ssh` + `tar` of `/data/ao/ao.db` (with `sqlite3 .backup`) and `hosts.json` to a local machine or an S3-compatible bucket.
- [ ] Redeploy policy: BFF endpoint `POST /hub/workers/:id/drain` → marks `draining`, the UI does not allow new sessions; when there are no active ones → the operator performs the redeploy. Document it.
- [ ] Check the daemon's behavior after a restart for sessions that were running: status, ability to resume (`docs/architecture.md` lifecycle/reaper).
