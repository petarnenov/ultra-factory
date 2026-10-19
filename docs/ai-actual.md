# Actual AI tool time on the CO project board

`Actual (ai4h)` on [AO: Hierarchical Coordinators (CO)](https://github.com/users/petarnenov/projects/2)
records what a task really cost, in the same four-hour blocks as
`Estimate (ai4h)`. The estimate is a judgement call; this column is measured.

## Why the measurement runs locally

The coding-agent session logs live on the machine that ran the tool, and a
GitHub runner cannot read them. The work is split in two:

1. `scripts/ai-actual/report.mjs` measures on that machine and dispatches the
   workflow with the numbers.
2. [`.github/workflows/ai-actual.yml`](../.github/workflows/ai-actual.yml)
   writes the field, rolls the value up into the parents, and comments the
   evidence on the issue.

## What counts as active time

Session logs are read from:

- Codex: `~/.codex/sessions/**/rollout-*.jsonl`
- Claude Code: `~/.claude/projects/*/*.jsonl`

Consecutive events closer than the idle cut are summed; a longer gap is time
the tool was not working on the task and is dropped. The default cut is five
minutes. The total is divided by four hours, so the column is in blocks:

```text
blocks = sum(min(gap_i, idle_cut)) / 4h
```

A single-event session measures zero. The first event starts the clock rather
than adding a fixed cost.

## Attribution

A session belongs to a pull request when the log itself names the head branch:
Claude Code records `gitBranch` on its entries, and Codex records
`payload.git.branch` in `session_meta`. Only the events recorded on that branch
are counted, so time spent on another branch in the same session is left out.

Two fallbacks cover logs that name neither the branch nor the pull request:
a worktree that still sits on the head branch, and an explicit `--cwd <path>`.
Claude Code also records `prNumber`, which matches on its own.

Events after the merge time are dropped, so a session left open afterwards does
not inflate the number. Sessions from an orchestrator or a root checkout never
match a feature branch, which is what excludes the largest long-running
sessions from every task.

`--verbose` prints every candidate and why it was skipped. Sessions under one
minute are counted as zero and left out of the evidence table.

The issue is taken from the pull request's closing keywords, including after
the merge: `closingIssuesReferences` empties on merge, so the title and body are
parsed as well. The pull request must carry `Closes #NNN`.

## Run it

```bash
npm run ai-actual -- --pr 282 --dry-run   # measure and print, dispatch nothing
npm run ai-actual -- --pr 282             # measure and dispatch the workflow
npm run ai-actual:test                    # unit tests, plus live checks with a token
```

| Option | Meaning |
| --- | --- |
| `--pr <number>` | Pull request to measure. Required. |
| `--issue <number>` | Record this issue instead of the ones the PR closes. |
| `--cwd <path>` | Extra working directory to attribute sessions from. |
| `--idle-cut <minutes>` | Gap treated as idle. Default 5. |
| `--ref <branch>` | Branch holding the workflow. Default: the default branch. |
| `--workflow <name>` | Workflow file name. Default `ai-actual.yml`. |
| `--dry-run` | Print the measurement without dispatching. |
| `--verbose` | Print every candidate session and its outcome. |

Until the workflow reaches the default branch, dispatch it from `dev` with
`--ref dev`.

## The record side

`scripts/ai-actual/record.mjs` runs in the workflow and does three things:
writes `Actual (ai4h)` on the issue's project item, sets every ancestor up to
three levels above to the sum of its children, and comments the evidence on the
issue. The roll-up skips ancestors with no measured children, so a story is
never zeroed by writing a sibling.

Its tests run in the workflow before the write, and the live checks validate the
queries against the project, so a broken query fails before it can write. The
same test job runs on every pull request that touches `scripts/ai-actual/`, with
the live cases skipping themselves there because no token is available.

## One-time setup

The workflow needs a token that can write to a project owned by a user
account. A fine-grained token cannot: GitHub lists user-owned projects as a
fine-grained gap, and the Projects permission exists only for organizations.
Create a **classic** token with the `project` scope and store it as the
repository secret `PROJECT_TOKEN`:

```bash
gh secret set PROJECT_TOKEN --repo petarnenov/ultra-factory
```

This repository is public, so `project` alone is enough; a private repository
would also need `repo`. A classic scope reaches every project the account
owns, so set an expiration and rotate it. The issue comment uses the workflow
token with `issues: write`, so the PAT does not need issue permissions.
Without the secret the run fails with a message naming it.

## Limits

- Only the coding tool's time is counted. Review, CI waits, queueing, and human
  decisions are outside the number, as they are outside the 4-hour cap.
- A task session left open inside its worktree counts up to the merge, even
  while nobody is typing.
- Sessions on a machine that is not the one running the report are invisible.
  Run the report on each machine that does work.
- The number is written by the workflow, not derived from the board, so it is
  never recomputed on its own. Re-running the report overwrites it.
