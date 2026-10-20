# ao pr

Request a merge of a pull request, resolve its review threads, or mark it ready for review.

## Syntax

```
ao pr <subcommand> [args] [flags]
```

## Subcommands

### ao pr merge

Request a squash merge of a tracked pull request through the owning SCM
provider.

**Syntax:**
```
ao pr merge <pr-number>
```

**Merge authority.** Only the project's root orchestrator, or a human, merges.
The CLI sends the calling session (`AO_SESSION_ID`) as `requestedBySessionId`,
and the daemon refuses a live coordinator or worker with
`403 MERGE_ROOT_ONLY` in the standard error envelope (the CLI prints the code
and the request id, and exits 1). Do not retry the merge; report the PR as
ready and let the root merge it. A caller that sends no session id, or one the
daemon does not know or has terminated, is not refused: this is an accident
guard, not an authorization boundary.

**Examples:**

```bash
ao pr merge 42
```

### ao pr resolve-comments

Resolve selected or eligible review threads on a pull request.

**Syntax:**
```
ao pr resolve-comments <pr-number> [comment-id...]
```

**Examples:**

```bash
ao pr resolve-comments 42
```

### ao pr ready

Mark a session's open pull request ready for review, or back to a draft with
`--undo`. This is the manual override of the project's CI gate.

**Syntax:**
```
ao pr ready [<session>] [--undo]
```

Only the project's root orchestrator and the session's owning coordinator may
run it; a worker is refused with `403 CI_GATE_FORBIDDEN`, so do not try to lift
the gate from a worker. A session with no open pull request fails with
`NO_OPEN_PR`; a pull request that already has the requested state is left
alone. It works with `--host`.

**Examples:**

```bash
ao pr ready demo-1
ao pr ready demo-1 --undo
```
