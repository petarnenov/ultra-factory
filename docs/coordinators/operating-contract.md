# Coordinator operating contract

This is the operating contract between the project root orchestrator and a
helper coordinator. It implements CO-3.2.1 on top of
[ADR 0010](../adr/0010-coordinator-hierarchy-and-report-ownership.md); where
this document and the ADR disagree, the ADR wins. Rules that the ADR does not
state are marked **(contract rule)**.

## Where the contract lives

| Part | Carried by | Who writes it |
| --- | --- | --- |
| Standing rules: scope, worker cap, no merge or closure, summaries, escalation reasons, handback | The coordinator system prompt in `backend/internal/session_manager/prompt.go` (`coordinatorSystemPrompt`). Every coordinator gets it, including after a relaunch. | Product |
| Assignment: scope, task list, branch rules, reporting cadence | The brief the root sends with `ao spawn --kind coordinator --scope <text> --prompt <brief>`, filled in from the template below. | Root, per coordinator |
| Root-side procedures: reassignment, replacement, termination | This document. | Root reads it |
| Rollout, monitoring, and rollback for the pilot | [Railway runbook: coordinator rollout and rollback](../../deploy/railway/README.md#coordinator-rollout-and-rollback-pilot). | Operator |

The system prompt is the enforced baseline. This document is the long form: it
explains the rules, gives the brief template, and defines what the root does.
If `--prompt` is omitted the daemon sends a generic one-line brief, so a root
should always send the assignment brief.

## Assignment brief template

Send this as the coordinator's `--prompt` (the daemon limits `--prompt` to
16 KiB). All five sections are required; a brief with a missing section is not
a valid assignment.

```text
Assignment for <coordinator name>.

Scope: <one bounded area, at most 200 characters; same text as --scope>
Out of scope: <areas and files this coordinator must not touch>

Task list (work these, nothing else):
1. <issue or task id> - <title>
   Acceptance criteria: <testable criteria>
   Files likely touched: <paths>
2. ...

Worker cap: <workerLimit> active workers at once (the value from `--max-workers`,
or the project's per-coordinator limit), and you may never exceed it. Wait for
a worker to finish before starting another.

Host placement: <host:slots, ...> (the value from `--hosts`), or the allowed
hosts the root names. Start every worker with `ao spawn --host <host>` on one
of these hosts. A refused placement is handled as in the system prompt.

Worker agent: <inherit | the profiles the daemon picks from, by label>. When
the root fixes or randomizes the agent, do not pass `--harness`, `--model` or
`--effort` to `ao spawn`.

Branch rules:
- Integration branch / PR base: <dev>
- Worker branch name: <prefix>/<task-id>
- Workers open pull requests; they never push to the integration branch.
- No force push, no history rewrite, no commits that name an AI tool.

Reporting cadence: use `ao coordinator report`. Send a summary now, on every
meaningful state change, when your scope is done, and whenever you are
blocked. No timer-based reports. One line per worker, not raw conversation.

You never merge and never close issues. A finished worker or a green pull
request means "ready for the root to integrate", not "done".

Ready pull requests: spawn workers with auto-review on (the default for your
workers). After a worker reports done, wait for the AO review verdict on its
current head and for green CI. Report a PR with
`ao coordinator report --ready-pr <number>@<head-sha>` only when the review
approved that head, CI is green, and no review thread is unresolved.
```

The root checks before sending: scope is non-empty and bounded, every task has
acceptance criteria, task file sets do not overlap with another coordinator's,
and the number of tasks that can run at once is at most the worker cap.

## Summaries

A coordinator reports with `ao coordinator report`, not `ao report`. `ao report`
is for workers; the daemon rejects it from a coordinator with
`REPORT_WORKER_REQUIRED`. A summary is addressed to the project root by role,
never by session ID, so a root replacement does not break it.

| Moment | Required `--state` | Content |
| --- | --- | --- |
| Assignment accepted | `planning` | The task list as understood, the planned order, and any question about the brief. |
| Meaningful state change | `working` | What changed: a worker started, a pull request opened, a task moved to review, a dependency resolved. Not every commit or message. |
| Scope complete | `done` | Per task: worker, pull request, head commit, checks. Stated as **ready for integration**. |
| Blocked | `blocked` | What is blocked, why, and what is needed; with `--escalate` when it needs the root (see below). |

Rules:

- Link the workers the summary covers with `--worker <id>` (at most three, all
  owned by the coordinator).
- Pass a stable `--submission-key` when retrying, so a retry returns the first
  summary instead of sending a second.
- Non-escalation summaries are batched by the daemon; an escalation reaches an
  active root immediately (ADR section 6).
- **(contract rule)** Do not send a summary when nothing changed, and do not
  paste raw worker conversation or logs. The root reads live state
  (`ao session ls`, `ao coordinator ls`, `gh pr`) for decisions; a summary is a
  convenience, not the source of truth.

## Escalation

An escalation is a `blocked` summary with an escalation reason. The coordinator
stops work on the affected item and keeps working on items that are not
affected until the root answers.

| Trigger | `--escalate` | When |
| --- | --- | --- |
| Overlapping files | `overlapping_files` | Two workers, or a worker and another coordinator, would change the same files. |
| Unclear acceptance criteria | `unclear_acceptance` | A task has missing or ambiguous criteria. Do not guess. |
| Failed worker | `worker_failed` | A worker failed, is stuck, or cannot finish its task. |
| Decision outside the scope | `scope_decision` | The work needs a decision or a change outside the assigned scope. |
| Anything else | `other` with `--note` | The note is required. |

The reason `coordinator_terminated` is written by the daemon, never by a
coordinator.

The root can spawn coordinators only in a project that enables them
(`ao project set-config <id> --coordinators`, `coordinators.enabled` in the
API). The feature is off by default: the daemon refuses a spawn with
`COORDINATORS_DISABLED`. `maxCoordinators`, `maxWorkersPerCoordinator` and
`maxWorkers` default to 2 / 3 / 6. A project may set any value up to the hard
maximum of 4 / 6 / 12 (a constant in the backend); a value above it or below
zero is refused with `COORDINATOR_LIMITS_INVALID`, as is a
`maxWorkersPerCoordinator` above the resolved `maxWorkers`. A coordinator may
have its own `workerLimit`, from 1 up to the project's
`maxWorkersPerCoordinator`: set at spawn, or changed on a live coordinator by
the root or by the operator on its behalf. Omitted means the project value.

Placement of coordinator-owned workers is enforced by the daemon:

- A coordinator-owned worker on the local daemon is refused with
  `COORDINATOR_LOCAL_WORKER_REFUSED` unless the project sets
  `coordinators.allowLocalWorkers` (default `false`). Root-owned workers are not
  affected. The rule applies to spawn, restore and reassignment; running local
  workers are grandfathered.
- `coordinators.allowedHosts` lists the paired remote host IDs a coordinator's
  workers may use; empty allows any paired host. It never names the local daemon.
- A coordinator may have `hostSlots` (`[{hostId, slots}]`, `slots >= 1`, one
  entry per host, a host ID or label as input, stored as IDs), set at spawn or
  changed live by the root or the operator through
  `PUT /api/v1/sessions/{id}/coordinator-limits`. The total may not exceed the
  coordinator's `workerLimit`; with slots set, remote workers run only on
  slotted hosts and the slot total is the effective placement limit for them.
  A local worker (when `allowLocalWorkers` is on) uses no slot: it counts
  toward `workerLimit` and the project totals only. A host outside the
  slots or `allowedHosts` is `COORDINATOR_HOST_NOT_ALLOWED`; a host whose slots
  are all used is `COORDINATOR_HOST_SLOTS_FULL`. Both are checked before the
  remote worker is created and again when its link is registered or reassigned.
  A slot is freed when the worker's link goes away, and an offline host keeps
  its slots counted until then. The roster shows each slot with its use.

## What a coordinator's prompt contains

- The worker cap shown to the coordinator is its own `workerLimit` when set,
  never above the project's effective `maxWorkersPerCoordinator`, read at spawn
  and restore. A live change sends the coordinator a short message with the new
  value. The daemon still enforces the real limit.
- A worker owned by a live coordinator is told to escalate to that coordinator's
  session id. A root-owned worker is told to message the root.
- The project's `orchestratorRules` are appended to the coordinator prompt after
  a precedence line: the coordinator restrictions override any rule below that
  conflicts with them.

## What a coordinator must not do

- Merge a pull request, or ask a worker to. The root alone merges.
- Close an issue, a story, an epic, or any project-wide item.
- Spawn another coordinator, or assign, reassign or adopt workers.
- Start a worker beyond the project's configured limits (6 in the project and
  3 under itself by default); the daemon refuses with `COORDINATOR_WORKER_LIMIT_REACHED` or
  `PROJECT_WORKER_LIMIT_REACHED`, and the coordinator waits instead of retrying.
- Take unrelated project tasks. Anything outside the assigned task list is a
  `scope_decision` escalation.

**Worker completion is not integrated completion.** A worker is complete when
its pull request is open, checks are green and it has reported. Work is
integrated only when the root has merged it and closed the tracking items.
Coordinators say "ready for integration" and stop there (ADR invariant I6).

Enforcement today: the prohibition is a prompt rule. The ADR names a daemon
guard, `MERGE_ROOT_ONLY`, for merge requests from a coordinator or worker; it
is not implemented on `dev` at the time of writing, so the prompt is the only
control until it lands.

## Root procedures

### Assigning work

Direct delegation still works: a worker spawned by the root is root-owned. A
worker spawned from a coordinator session is owned by that coordinator. To move
an existing worker: `ao session assign <worker> --owner <coordinator-id|root>`
(root only; the target must be live and under its three-worker cap).
A linked remote worker is identified by its host and remote session id, so use
`ao remote-worker assign <host-id> <remote-session-id> --owner <coordinator-id|root>`
instead, and list them with `ao remote-worker ls --project <id>`. The ADR
sketches this as `ao session assign --host --remote-session`; the shipped
command is `ao remote-worker assign` (see `docs/cli/README.md`). Every
assignment step below applies to local workers and remote links alike.

Capacity the root plans around (ADR section 8, enforced by the daemon): two
live coordinators, three active workers per coordinator, six active workers in
the project. Root-owned workers count toward the six, so two coordinators with
three workers each leave no room for a root-owned worker, and a spawn beyond
the limit fails with `PROJECT_WORKER_LIMIT_REACHED` until a worker terminates.
Spawning coordinators also requires `coordinators.enabled` in the project
configuration (`COORDINATORS_DISABLED` otherwise). Reassigning to the root
never changes the project total.

### Reassigning a worker

- Reassignment is one durable owner write plus audit rows. The worker, its
  session, its worktree and its reports are not deleted or restarted.
- Pending reports route to the new owner on the next claim.
- While a report of that worker is claimed for the old owner the write fails
  with `OWNER_REASSIGNMENT_IN_FLIGHT`; retry shortly.
- The old coordinator gets an ownership-removed notice and must stop acting on
  the worker.

### Replacing a coordinator (reassigned or terminated)

The project allows at most two live coordinators, and a terminated coordinator
frees its slot at once. There is no capacity exception, so the replacement can
only be spawned when a slot is free.

1. **Planned, with a free slot** (fewer than two live coordinators). Spawn the
   replacement with the same scope and a brief listing the remaining tasks.
   Reassign each live worker with
   `ao session assign <worker> --owner <new-coordinator-id>` (at most three).
   Then stop the old coordinator with `ao session kill <old-id>`. Workers keep
   running throughout.
2. **Planned, at capacity** (two live coordinators). Stop the old coordinator
   first with `ao session kill <old-id>`. The daemon returns its workers to the
   root (see the automatic path below), which frees the slot. Then spawn the
   replacement and assign the returned workers to it with
   `ao session assign <worker> --owner <new-coordinator-id>`, using
   `ao remote-worker assign <host-id> <remote-session-id> --owner
   <new-coordinator-id>` for each returned remote link. Between the kill and
   the assignment the workers are root-owned and keep running; their reports go
   to the root.
3. **Automatic.** If the coordinator terminates (kill, reaper, PR-merge lifecycle, replacement),
   the daemon in one transaction returns every worker and remote link it owned
   to the root and writes a root-addressed `blocked` summary with
   `escalation_reason=coordinator_terminated` listing them. The root then
   decides: take the workers itself, or spawn a replacement and assign them.
4. Reports of the returned workers and the coordinator's pending summaries stay
   durable and are delivered to the root.
5. Restoring a terminated coordinator does **not** reattach its former workers;
   the root assigns them explicitly if wanted. A restore re-checks the
   two-coordinator cap and fails with `COORDINATOR_LIMIT_REACHED` when two are
   live.
6. Root replacement does not touch coordinators: they keep running and keep
   their workers; pending summaries go to the new root.

**(contract rule)** The replacement's brief must restate the task list with
the current state of each task (worker, branch, pull request), because the
previous coordinator's conversation is not available to it.

## Acceptance criteria mapping (CO-3.2.1)

| Criterion | How the contract meets it |
| --- | --- |
| A coordinator manages three workers without unrelated project tasks | Cap of three in the brief and system prompt (daemon-enforced); task list and out-of-scope sections; out-of-scope work is a `scope_decision` escalation. |
| The root gets summaries, not raw worker conversation | Summary triggers and the no-raw-conversation rule; daemon batching of non-escalation summaries. |
| Worker completion differs from integrated completion | "Ready for integration" wording; merge and closure prohibition. |
| Exercisable in the pilot without manual forwarding | `ao coordinator report` and owner-routed reports are daemon features (CO-2.2.2); the contract needs no manual forwarding. |

CO-4.1.1 should exercise one successful and one blocked worker handoff against
this contract.

## Agent profiles

`coordinators.agentProfiles` is a project list of `{id, label, harness, model,
effort, weight}`. Three selections draw from it, each `inherit` (the default,
today's behavior), `fixed` (one profile) or `random` (a weighted pick from a
pool, or from every profile when the pool is empty):

- `coordinatorAgent` picks a new coordinator's own agent. A spawn can override
  it per coordinator; explicit `--agent`, `--model` and `--effort` must then match
  an allowed profile, or the spawn is refused with
  `COORDINATOR_PROFILE_NOT_ALLOWED`.
- `workerAgent` is the default for the workers a coordinator spawns. A
  coordinator can have its own selection, set at spawn or changed live by the
  root (`ao coordinator set --worker-agent`); a change applies to later spawns
  only and is audited in `coordinator_agent_events`.
- `rootWorkerAgent` applies to workers the root spawns directly. The root's
  explicit flags always win there.

A coordinator's explicit `--agent`, `--model` or `--effort` is accepted only
when it matches a profile in its selection (`WORKER_PROFILE_NOT_ALLOWED`
otherwise). The daemon never picks a profile whose harness is definitely not
installed or not authorized on the target machine, and refuses with
`WORKER_PROFILE_UNAVAILABLE` (nothing is spawned) when none is left. For
`ao spawn --host`, the CLI reports the host's readiness to the local daemon,
which makes the pick. The chosen profile is recorded on the session and shown
in the roster, and a restore keeps it without re-rolling.
