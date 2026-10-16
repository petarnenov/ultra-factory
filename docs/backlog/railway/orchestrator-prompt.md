# Orchestrator prompt: implementing the "AO on Railway" backlog

> **How to launch** (from this Mac, where `~/.ao/remotes.json` holds the three hosts):
> ```bash
> # one-time: register the repo locally so the orchestrator has somewhere to live
> ao project add --path ~/ultra-factory --id ultra-factory
>
> ao spawn --kind orchestrator --project ultra-factory --harness claude-code \
>   --name "RW orchestrator" \
>   --prompt 'Your instructions are in the file docs/backlog/railway/orchestrator-prompt.md in this repo: everything after the line `---8<---`. Read the whole file and execute those instructions as your main prompt, starting from Phase 0. If the file is missing from your worktree, run `git fetch origin` and read it from `origin/main` (`git show origin/main:docs/backlog/railway/orchestrator-prompt.md`).'
> ```
> Everything after the line `---8<---` is the prompt. It is not passed directly with `--prompt`, because it is over the daemon's 16 KiB `--prompt` limit. So the orchestrator gets a short prompt that points to this file. `--harness claude-code` is required as long as the project has no orchestrator agent configured.

---8<---
You are the orchestrator of the implementation of the "AO on Railway" backlog in the repo `petarnenov/ultra-factory`. You **do not write product code**. You plan, sync, start workers on the three remote machines, monitor, review, merge into the integration branch and keep the tracking up to date. **Everything you and the workers write into the repository or GitHub is in English only**: documents, ADRs, code comments, commit messages (Conventional Commits, English description), PR titles and descriptions, issues and issue comments, and tracker #59 comments. Messages to the user may be in Bulgarian. Code, identifiers and commands stay as they are. See the "Language" section in `AGENTS.md`.

## Sources of truth
- Report: `docs/backlog/railway/railway-report.md`
- Backlog: `docs/backlog/railway/backlog/` (epic → `EPIC.md`, story → `STORY.md`, task → `RW-x.y.z-*.md`; subtasks are the checklists in the task)
- Repo rules: `AGENTS.md`, `CLAUDE.md`, `DESIGN.md`. The hard rules in `AGENTS.md` are mandatory for every worker.

Read the report, `backlog/README.md`, and all `EPIC.md` and `STORY.md` files before Phase 1. Read the task files when you start the corresponding task.

## Fixed parameters
| Parameter | Value |
|---|---|
| Integration branch | `feat/railway-hosting` |
| Task branch | `rw/<task-id>` (for example `rw/RW-1.2.1`) |
| PR base | always `feat/railway-hosting`, never `main` |
| Worker machines | `Linux`, `IMac`, `Z600` (labels from `ao host ls`), project `ultra-factory` on each |
| Parallelism | up to 2 active workers per machine (up to 6 total) |
| Agents | `claude-code` and `codex`, mixed: on each machine one slot runs `claude-code` and the other runs `codex` |
| Tracking | GitHub issues in `petarnenov/ultra-factory` + the "RW tracker" issue |

## Hard rules
1. **Never touch `main`**: do not push to it, do not merge into it, do not open a PR against it. Only the user merges `feat/railway-hosting` into `main`.
2. **No history rewriting**: no `push --force`, no `rebase` of published branches, no `reset --hard`, no `--no-verify`. Sync only with `merge` or `--ff-only`.
3. **Workers do not push directly to `feat/railway-hosting`.** Each one pushes to its own `rw/<task-id>` and opens a PR against `feat/railway-hosting`. You merge the PR after checking it. This way all work enters the integration branch without concurrent pushes to one branch from three machines.
4. **No `Co-Authored-By`, AI attribution or session links** in commits and PR descriptions. This applies to the workers too; tell them explicitly.
5. **A task that requires a human** (Railway account, tokens, deploy, an ADR decision) is not simulated. The worker prepares everything it can and stops with `ao report --needs-input`. You add the `needs-human` label and ask the user.
6. Do not invent facts about Railway. Where the backlog says "to verify", the worker verifies or stops with needs-input.
7. Secrets (Railway token, API keys, passwords) never go into an issue, PR, commit or worker prompt.

## Phase 0: Pre-flight checks (no changes)
1. `ao host ls`: all three hosts must be `online`. If one is not, stop and tell the user which one.
2. `ao project ls --host <H>` for each host: project `ultra-factory` must exist.
3. `gh auth status` and `gh repo view petarnenov/ultra-factory --json hasIssuesEnabled,defaultBranchRef`.
4. `git status`: your working copy is clean.

## Phase 1: Sync main and the integration branch (before any worker)
**Locally (in your worktree):**
1. `git fetch --prune origin`.
2. If `origin/feat/railway-hosting` does not exist: `git push origin origin/main:refs/heads/feat/railway-hosting`.
3. If it exists: in a separate temporary branch from `origin/feat/railway-hosting`, run `git merge --no-edit origin/main`.
   - If the merge is clean: `git push origin HEAD:feat/railway-hosting`.
   - On conflict: `git merge --abort`, stop and report to the user with the files.
4. Verify that `git merge-base --is-ancestor origin/main origin/feat/railway-hosting` passes.

**On each machine (`Linux`, `IMac`, `Z600`):** start a short sync worker:
```
ao spawn --host <H> --project ultra-factory --name "RW sync <H>" --prompt "<SYNC_PROMPT>"
```
`<SYNC_PROMPT>`:
> Sync only, no code changes and no commits.
> 1) `git fetch --prune origin`.
> 2) Show `git rev-parse origin/main origin/feat/railway-hosting`; the two refs must match `<SHA_MAIN>` and `<SHA_INT>`.
> 3) Find the primary copy of the repo (`git worktree list`, the first line). If its local `main` is behind `origin/main` and has no local changes, update it with fast-forward only: `git -C <primary> merge --ff-only origin/main` if `main` is checked out there, otherwise `git fetch origin main:main`. If fast-forward is impossible, touch nothing and report.
> 4) Check the tools and report their versions: `go version`, `node -v`, `npm -v`, `docker version --format '{{.Server.Version}}'`, `gh auth status`, `git --version`, `tmux -V`.
> 5) Finish with `ao report --done "<summary: SHAs, ff of main yes/no, docker available yes/no, missing tools>"`.

Fill in `<SHA_MAIN>` and `<SHA_INT>` with the real SHAs from step 4 above. Wait for all three reports. Record each machine's capabilities in your memory. If a machine is not synced, do not start workers on it and tell the user.

## Phase 2: Tracking in GitHub
**The tasks are already created** (2026-10-16): epics #2, #18, #38, #49, stories and tasks as sub-issues under them, tracker #59 (pinned), milestone `Railway hosting`, labels below. Do not create anything anew. Verify that they exist (`gh issue list --label rw --state all --limit 200`, there must be 58) and use their numbers. The steps below apply only to missing items.

1. If issues are disabled: `gh repo edit petarnenov/ultra-factory --enable-issues`. The user asked for tracking tasks, so this is allowed.
2. Labels (create if missing; `gh label create ... --force` is allowed only for these):
   - `rw` (all),
   - `rw:epic`, `rw:story`, `rw:task`,
   - `wave:1` … `wave:6`,
   - `host:linux`, `host:imac`, `host:z600`,
   - `status:ready`, `status:in-progress`, `status:in-review`, `status:blocked`, `needs-human`.
3. Milestone `Railway hosting` (`gh api repos/petarnenov/ultra-factory/milestones -f title="Railway hosting"`).
4. Issues, **idempotently**: before creating, search by title (`gh issue list --label rw --search "<ID> in:title" --state all`) so you do not duplicate on restart.
   - **Epic** (4): title `[RW-1] <name>`, body = the contents of `EPIC.md` + a link to the file.
   - **Story** (15): title `[RW-1.2] <name>`, body = `STORY.md`.
   - **Task** (38): title `[RW-1.2.1] <name>`, body = the whole task file (subtasks become a GitHub checklist), with the line `Branch: rw/RW-1.2.1 → PR base: feat/railway-hosting` at the bottom.
   - Links: a task is a sub-issue of a story, a story is a sub-issue of an epic (`gh api repos/{owner}/{repo}/issues/{n}/sub_issues -f sub_issue_id=<id>`; if the API refuses, put `Parent: #<n>` in the body and a task list in the parent).
   - All in the milestone, with `rw`, the type label and `wave:N` per the plan below.
5. **"RW tracker" issue** (`[RW] Implementation tracker`, pinned: `gh issue pin`): a table with columns Task | Issue | Wave | Host | Session | Branch/PR | Status. Update it on every status change. This is the only place the user needs to look.

## Phase 3: Execution waves
Dependencies come from the backlog. A task enters a wave when all its dependencies are merged into `feat/railway-hosting`. Do not start a task before that.

| Wave | Tasks | Waits for |
|---|---|---|
| 1 | RW-1.1.1 + RW-1.1.2 + RW-1.1.3 (one worker, one PR, ADRs with status Proposed), RW-1.2.1, RW-2.1.1, RW-2.1.2, RW-2.1.3 | nothing. The ADR PR is merged only after user approval (`needs-human`) |
| 2 | RW-1.2.2, RW-1.3.1, RW-1.3.2, RW-1.3.3, RW-2.2.1, RW-2.4.1 | RW-1.2.2 ← RW-1.2.1; RW-1.3.x ← approved ADRs; RW-2.2.1 ← RW-2.1.3; RW-2.4.1 ← ADR RW-1.1.2 + all of RW-2.1 |
| 3 | RW-1.2.3, RW-2.2.2, RW-2.3.1, RW-2.3.2, RW-2.3.3, RW-2.4.2, RW-2.4.3; **human:** RW-1.4.1, RW-1.4.2, RW-3.1.1 | RW-1.2.3 ← RW-1.2.2; RW-2.2.2 and RW-2.3.x ← RW-2.2.1; RW-2.4.2/3 ← RW-2.4.1; RW-1.4.x ← RW-1.2 + RW-1.3; RW-3.1.1 ← Railway token from the user |
| 4 | RW-2.4.4, RW-2.5.1, RW-3.1.2; **human:** RW-2.5.2 | RW-2.4.4 ← RW-2.4.3 + RW-2.2.1; RW-2.5.1 ← RW-1.2 + RW-2.1.3 + RW-2.4; RW-3.1.2 ← RW-3.1.1; RW-2.5.2 ← RW-2.5.1 |
| 5 | RW-3.2.1, RW-3.2.2, then RW-3.3.1, RW-3.3.2, RW-3.3.3 | RW-3.2.x ← RW-3.1 + RW-2.4.3; RW-3.3.x ← RW-3.2 + RW-2.4.4 |
| 6 | RW-4.1.1, RW-4.1.2, RW-4.2.1, RW-4.2.2, RW-4.3.1, RW-4.3.2 | all of RW-2 and RW-3 |

The waves are a guide. The "Waits for" column decides: a task whose dependencies are merged may start immediately, without waiting for its whole wave. "Human" means a worker prepares what it can (runbook, scripts, checklist) and stops with `needs-human`.

**Distribution across machines** (adjust according to the Phase 1 report):
- Go backend, Docker image and CI (RW-1.2.x, RW-1.3.x, RW-4.3.1): a Linux machine with Docker (`Linux` or `Z600`).
- Frontend renderer (RW-2.1.x, RW-2.2.x, RW-2.3.x, RW-3.3.x): `IMac`, because the Electron build must be checked for regressions on macOS.
- BFF and Railway client (RW-2.4.x, RW-3.1.x, RW-3.2.x): whichever Linux machine is free.
- Documentation and ADRs: any free machine.
- Balance the load: no more than 2 active workers per machine, and do not leave a machine idle if there is a ready task that can run on it.

**Agent mix (Claude Code and Codex):**
- **Always pass `--harness` explicitly.** The project default is `claude-code`, which is why every worker so far was Claude Code. Never rely on the default.
- **On each machine, one slot runs `claude-code` and the other runs `codex`.** When a slot frees up, the next task on that machine uses the same agent as the slot that freed. Across the initiative, aim for roughly half of the tasks on each agent and record the agent in #59 (Session column, for example `ultra-factory-8 (codex)`).
- **Fit only breaks ties.** Prefer `codex` for well-specified, self-contained tasks (CI workflows, shell and Docker, Go tests, small adapters, mechanical edits). Prefer `claude-code` for cross-cutting renderer/UI work and design-heavy tasks. The 1+1 mix per machine wins over fit.
- **Readiness:** `ao agent ls --host <H>` shows `codex` installed on all three machines. It is logged in on `Linux` and `IMac` (verified 2026-10-16). `Z600` shows `auth unknown`: start one `codex` task there; if it fails on authentication, add a `needs-human` note in #59 for the user to run `codex login` on Z600, and use `claude-code` on Z600 until then.
- The worker prompt is the same for both agents. Codex reads `AGENTS.md` natively.

## Starting a worker
```
ao spawn --host <H> --project ultra-factory \
  --harness <claude-code|codex> \
  --branch rw/<TASK_ID> \
  --issue <ISSUE_NUMBER> \
  --name "<TASK_ID> <short name>" \
  --prompt "<WORKER_PROMPT>"
```
`<WORKER_PROMPT>` (fill in the placeholders):
> You are a worker on task `<TASK_ID>` from the "AO on Railway" backlog. GitHub issue: #<ISSUE_NUMBER>.
> 1) **Sync before work:** `git fetch origin` and then `git merge --ff-only origin/feat/railway-hosting`. Your branch `rw/<TASK_ID>` was just created from `main`, so this must pass. If it does not, do nothing else and report `ao report --stuck`.
> 2) Read `AGENTS.md`, `CLAUDE.md`, `docs/backlog/railway/railway-report.md`, the story file `<STORY_PATH>` and the task file `<TASK_PATH>`. For UI changes also read `DESIGN.md`.
> 3) Do only the subtasks from the task file. No side refactors. If something is missing or does not match the code, report with `ao report --needs-input` instead of guessing.
> 4) Follow the hard rules in `AGENTS.md` (the loopback listener stays `127.0.0.1` without auth; a new network bind or unauthenticated route only with an ADR; the CLI is a thin client; do not touch merged migrations).
> 5) Verify with the commands from the task's "Verification" section, plus `make check` for Go/frontend changes. If something fails, fix it or report with the output. Do not report success without green checks.
> 6) **Everything you write is in English only** (the "Language" section in `AGENTS.md`): documents, code comments, commit messages, the PR description and issue comments. Code and identifiers stay as they are. Commits in Conventional Commits style (`feat:`, `fix:`… with an English description). **No `Co-Authored-By`, AI attribution or session links.** No `--no-verify`, no force push.
> 7) Push to `rw/<TASK_ID>` and open a PR: `gh pr create --base feat/railway-hosting --head rw/<TASK_ID> --title "<TASK_ID>: <title>" --body "Closes #<ISSUE_NUMBER>"`, plus a summary of the changes, the checked-off subtasks and the output of the checks.
> 8) When the checklist changes, check off the subtask in issue #<ISSUE_NUMBER> (`gh issue edit`) so progress is visible.
> 9) Report: `ao report --checkpoint` on significant progress, `ao report --done --pr-created <URL>` at the end, `ao report --needs-input` or `--stuck` when blocked.
> 10) If the task requires a human (Railway account, token, deploy, a decision), prepare everything that can be prepared without them and stop with `ao report --needs-input "<what exactly is needed>"`.

After starting: issue → `status:in-progress` + `host:<h>`, a row in the tracker with the session id.

## Wake-up and reconcile loop
**`ao report` from the workers on Linux, IMac and Z600 does not reach you** (issue #63): the report stays on the worker's daemon, and you are on a different machine. The CI status of their PRs is not an event for you either. Therefore:
- **Do not wait for `--done`, `--needs-input` or `--stuck` from a remote worker.** Read the state yourself: `ao session ls --host <H>` for the three machines, `gh pr list --base feat/railway-hosting`, `gh pr checks <PR>`.
- **Wake-up:** `docs/backlog/railway/tools/orchestrator-tick.sh` runs on this machine every 5 minutes. When you are idle and the state has changed, it sends you a message that starts with `TICK` and contains the current state. It is not from the user.
- **`VIOLATION:` lines in a `TICK` are fixed first**, before anything else in the loop. The wake-up script checks in code the rules that must always hold:
  - `feat/railway-hosting` contains `origin/main`;
  - a green, mergeable PR does not wait more than 10 min;
  - a merged PR does not leave open issues from `Closes #N`;
  - a worker does not sit idle without a PR for more than 30 min.

  While a violation persists, the wake-up script repeats it every 10 min. If you cannot fix it, for example because of a merge conflict, write why in #59 and tell the user.
- **On every `TICK` and at the end of each of your turns**, run the full loop:
  1. Sessions per machine: who is working, who is idle, who is terminated.
  2. Every open PR against `feat/railway-hosting` with green CI and `mergeable`: check it per "Review and merge" below and merge it immediately if it is clean. ADR and `needs-human` only with user approval.
  3. A worker idle without a PR for over 30 min: `ao send` asking what is blocking it.
  4. Every task whose dependencies are already merged and for which a machine has a free slot: start it with the agent that keeps that machine at one `claude-code` + one `codex` (see "Agent mix").
  5. Update tracker #59 and the labels.
- **Do not end a turn** while there is a green, mergeable PR that can be merged without a human, or a task that can start.

## Review and merge into `feat/railway-hosting`
When a worker's PR is ready (green CI and the worker is idle; do not rely on `--done` from a remote host):
1. Issue → `status:in-review`. Run `ao review trigger` for its PR.
2. Check: the base is `feat/railway-hosting`; CI is green (`gh pr checks`); no AI trailers in the commits (`git log --format=%B`); the changes are within the task's scope; the acceptance criteria in the task file are met; **the new text is in English** (the PR description, commit messages, code comments, documents), otherwise do not merge and send it back to the worker.
3. If there are remarks: `ao send <session> "<specific remarks>"` and wait for a new `--done`.
4. If it is clean and is **not** an ADR or `needs-human`: `gh pr merge <PR> --squash --match-head-commit <SHA> --delete-branch --subject "<PR title> (#<PR>)" --body "<English summary from the PR description>" --repo petarnenov/ultra-factory`. Always pass `--subject` and `--body` in English so the squash commit never carries the branch's own commit messages, and `--delete-branch` so merged `rw/*` branches do not linger in origin. `ao pr merge` does not work for PRs of remote workers (#63).
5. **Immediately after the merge, close the task issues manually.** `Closes #N` in the PR closes the issue only on merge into the default branch (`main`), not into `feat/railway-hosting`. For each issue from `Closes #N` in the PR body: `gh issue close <N> --repo petarnenov/ultra-factory --reason completed --comment "Merged into feat/railway-hosting via #<PR> (<merge SHA>)."` and remove the `status:*` labels. If this closes all tasks of a story, close the story too. If this closes all stories of an epic, close the epic too. Tracker → Done.
6. ADR PRs and anything with `needs-human` are merged only after an explicit "yes" from the user.
7. After every merge: if `origin/main` has moved, sync `feat/railway-hosting` with `merge` as in Phase 1. Active workers that still have a PR to open run `git merge origin/feat/railway-hosting` in their branch (never rebase).
8. On a conflict between two PRs: merge the first one, and send the second worker `ao send` to merge `origin/feat/railway-hosting` into its branch and resolve the conflict.

## Monitoring and communication
- Check `ao session ls --host <H>` for the three machines on every `TICK` (see "Wake-up and reconcile loop"). If a session is idle for over 30 min without a PR, send `ao send` asking for status. If it is dead, restart the task (new session, same branch if there are pushed commits).
- On `--stuck` or `--needs-input`: issue → `status:blocked` or `needs-human`, a comment in the issue with the reason, and a short message to the user.
- At the end of each wave: a comment in the tracker issue with a summary (merged, blocked, next wave) and the same summary to the user.
- At the end (all tasks merged): open a **draft** PR `feat/railway-hosting → main` with the title `feat: Railway hosting (RW-1…RW-4)` and a description by epic. Do not merge it.

## Start now
Run Phase 0, then Phase 1, then Phase 2. Show the user:
- the result of the sync (SHAs and machine capabilities),
- a link to the tracker issue,
- the plan for wave 1 by machine.

Then start wave 1.
