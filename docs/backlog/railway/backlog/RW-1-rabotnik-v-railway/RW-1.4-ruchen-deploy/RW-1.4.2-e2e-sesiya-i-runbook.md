# RW-1.4.2 · Task: End-to-end session from the desktop app and runbook
**Status:** ⛔ needs-human (#83)

**Type:** operations + documentation. **Estimate:** 0.5 pd. **Story:** RW-1.4.

## Context
- Desktop: Settings → Remote hosts (`frontend/src/renderer/components/settings/RemoteHostsSettings.tsx`), `~/.ao/remotes.json`.
- CLI: `ao host ls`, `ao spawn --host`.
- Credentials come from the worker's env, not from the laptop (`docs/self-hosted-remote.md`).

## Subtasks
- [ ] Add the host in the desktop app with URL and password; check that the `host.$hostId` route appears.
- [ ] Clone a project on the worker (`/api/v1/projects/clone` via the UI), start a Claude Code session, open a terminal, send a message, make a change, create a PR via `gh`.
- [ ] Check the `*.localhost` preview and artifacts through the remote proxy; record what does not work.
- [ ] Repeat the key steps with the CLI: `ao spawn --host worker-1 ...`, `ao session ls --host worker-1`.
- [ ] Runbook `deploy/railway/README.md`: prerequisites, env variables, adding it in the desktop app, password rotation, redeploy, what is lost on restart, troubleshooting (logs, `ao remote-host status` via `railway ssh`).

## Acceptance criteria
- [ ] Another person can repeat the procedure from the runbook without help.
- [ ] A "does not work / partially works" list is recorded and carried over as notes into RW-2 and RW-4.
