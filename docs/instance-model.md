# Daemon instances on one machine

A machine can run more than one AO daemon. The usual pair is the production
daemon (`~/.ao`, launchd `dev.ao.daemon`) and the dev daemon the desktop app
starts (`~/.ao/dev`, port 3002). This page says what each daemon keeps for
itself, what the machine shares, and what belongs to a project.

## Per instance

Each daemon has its own data dir (`AO_DATA_DIR`, default `<state>/data`):

- the SQLite database: sessions, reports, orchestrator state, and remote worker
  links (`remote_worker_links`, `remote_worker_report_deliveries`);
- the daemon's identity (`mobile/identity.json` in its data dir), which is its
  host ID and the puller ID it sends to remote daemons;
- its run file (`AO_RUN_FILE`, default `~/.ao/running.json`) and its port;
- worktrees and agent state.

The databases are never shared between instances. Two instances on one machine
never write the same SQLite file.

## Per machine

- The saved-host registry, `~/.ao/remotes.json` (`AO_REMOTES_FILE` overrides
  it). Every daemon and the desktop app read and write this one file under the
  `0600` rule and a lock file beside it. An instance's older local copy is
  merged into it when the daemon starts.
- The desktop app's own settings, and the choice of which daemon it starts.

## Per project

- The project and its sessions, which each instance registers itself.
- The remote workers of a project. A worker linked by one instance is seen by
  every instance that has the same project and host, once discovery runs there
  (issue #453 and the project-scoped link identity from #455 part 2). Each
  instance keeps its own copy of the link in its database.
- The reports of a remote worker. Each instance pulls them independently. A
  report stays pending for each puller until that puller acknowledges it, so
  every instance's orchestrator receives every report. Reports older than 7 days
  (or beyond the newest 10 000 for a worker) expire, and each instance counts the
  reports it missed (see [ADR 0005](adr/0005-daemon-pulls-remote-worker-reports.md),
  the per-puller amendment).

## Practical rules

- Use `AO_REMOTES_FILE` in tests, not the real `~/.ao/remotes.json`.
- An isolated install (`AO_DATA_DIR` set to any other directory) keeps its own
  `remotes.json` in that directory, so a lab never reads the real credentials.
- Pair a host once in the desktop app; both instances see it.
