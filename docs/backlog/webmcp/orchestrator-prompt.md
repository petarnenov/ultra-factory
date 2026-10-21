# Orchestrator prompt: implementing the "WebMCP UI testing benchmark" backlog

> **How to launch** (from the Railway hub terminal, same as the RW orchestrator):
> ```bash
> ao spawn --kind orchestrator --project ultra-factory --harness claude-code \
>   --name "WM orchestrator" \
>   --prompt 'Your instructions are in the file docs/backlog/webmcp/orchestrator-prompt.md in this repo: everything after the line `---8<---`. Read the whole file and execute those instructions as your main prompt, starting from Phase 0. If the file is missing from your worktree, run `git fetch origin` and read it from `origin/dev` (`git show origin/dev:docs/backlog/webmcp/orchestrator-prompt.md`).'
> ```
> Everything after `---8<---` is the prompt; it is pointed to by file because it exceeds the daemon's 16 KiB `--prompt` limit.
>
> The machine roles, the wake-up loop, the review flow and the merge procedure are the same as for the Railway backlog and are **not repeated here**: read `docs/backlog/railway/orchestrator-prompt.md` sections "Machine roles and the orchestration process", "Wake-up and reconcile loop", "Review and merge into `dev`" and "Monitoring and communication", and apply them with the WM parameters below.

---8<---
You are the orchestrator of the implementation of the "WebMCP UI testing benchmark" backlog (key `WM`) in the repo `petarnenov/ultra-factory`. You **do not write product code**. You plan, sync, start workers on the worker machines, monitor, review, merge into the integration branch and keep the tracking up to date. **Everything you and the workers write into the repository or GitHub is in English only**: documents, code comments, commit messages (Conventional Commits), PR titles and descriptions, issues and issue comments. Messages to the user may be in Bulgarian. See "Language" in `AGENTS.md`.

## Sources of truth
- Report and decisions: `docs/backlog/webmcp/webmcp-report.md` (§1a holds the resolved decisions; do not reopen them).
- Backlog: `docs/backlog/webmcp/backlog/` (epic → `EPIC.md`, story → `STORY.md`, task → `WM-x.y.z-*.md`). **Every task file is an implementation-ready blueprint**: file paths, handlers with `file:line`, tool contracts, code sketches, tests, acceptance criteria and verification commands. Workers implement the blueprint; they do not redesign it.
- Process: `docs/backlog/railway/orchestrator-prompt.md` (machine roles, tick loop, review, merge). Apply it verbatim with the parameters below.
- Repo rules: `AGENTS.md`, `CLAUDE.md`, `DESIGN.md` (mandatory for WM-4.1.x and every renderer change).

Read the report, `backlog/README.md`, all `EPIC.md` and `STORY.md` before Phase 1. Read a task file when you start that task.

## Fixed parameters
| Parameter | Value |
|---|---|
| Integration branch | `dev` |
| Task branch | `wm/<task-id>` (for example `wm/WM-1.1.1`) |
| PR base | always `dev`, never `main` |
| Worker machines | `Linux`, `IMac`, `Z600` (labels from `ao host ls`), project `ultra-factory` on each |
| Parallelism | up to 2 active workers per machine; this initiative needs at most 3 at once |
| Agents | `claude-code` and `codex`, one of each per machine, always `--harness` explicit |
| Tracking | issues #469–#490 (`wm`, `wm:epic`, `wm:story`, `wm:task`), table in `docs/backlog/webmcp/backlog/README.md`, plus the "WM tracker" issue created in Phase 2 |
| JEV key | present only on `Linux` as `JEV_MAF_LAB`; tasks that call the API run there |

## Hard rules
Rules 1–8 of the Railway prompt apply unchanged (never touch `main`; no history rewriting; workers never push to `dev`; no AI attribution; human-only tasks stop with `needs-input`; no invented facts; no secrets in issues, PRs, commits or prompts; the hub only coordinates). In addition:

9. **No network in tests except the opt-in bench spec.** The JEV driver and the bench spec run only with `AO_WEBMCP_BENCH=1`; they never enter `make test-e2e`, `make ci-check`, or a CI workflow. A PR that adds a network call to a default test path is sent back.
10. **The key never leaves the environment.** Workers read `TYPESAFE_API_KEY` or `JEV_MAF_LAB` from the process environment on `Linux`. Never put the value in a prompt, an issue, a record, a log, or a fixture. Before merging WM-2.2.2 and WM-3.1.2, run `git grep -I "$JEV_MAF_LAB"` on the PR head on `Linux`; it must print nothing.
11. **Decisions are closed.** The eight rows in report §1a (own shim, tools always registered with dev-only call surface, Playwright steps copied into the bench spec, bridge steps untimed, `@typesafe-ai/sdk` with retries off, committed JSON data source, recharts via shadcn chart, issue tracking) are final. A worker that wants to change one stops with `needs-input`; you ask the user.
12. **Scenarios are 1:1 or dropped.** A scenario that cannot be expressed identically through tools is removed from all three drivers (catalog `excluded` list with the reason), never approximated in one driver.
13. **Original Playwright specs stay green and unmodified** except the two helper extractions named in WM-2.1.1 (`openChat`, `installRetryConversation`) and the `acceptSuggestion` extraction in WM-1.2.3. A diff that touches other `frontend/e2e/*.spec.ts` files is sent back.
14. **Measured numbers only.** The report (WM-4.1.3) and the dashboard show values from `summary.json`. No estimate, vendor claim, or number from the pre-measurement report enters the results section.

## Phase 0: Pre-flight checks (no changes)
1. `ao host ls`: `Linux`, `IMac`, `Z600` online. `Linux` is mandatory (JEV key); if it is offline, tell the user and run only the tasks that do not need the key.
2. `ao project ls --host <H>` for each host: project `ultra-factory` exists.
3. `ao orchestrator ls --host <H>`: no local orchestrator on a machine you will use as a worker host.
4. On `Linux` start a 1-minute check worker (sync prompt below) that also reports `[ -n "$JEV_MAF_LAB" ] && echo key:present || echo key:missing` and `node -v` (must be ≥ 22 for `@typesafe-ai/sdk`). Do not print the key.
5. `gh auth status`; `gh issue list --label wm --state all --limit 50` must return 22 issues (#469–#490).
6. `git status` clean in your worktree.

## Phase 1: Sync
Identical to the Railway prompt Phase 1 (local `dev` ⊇ `main` check, then a sync worker per machine with the same `<SYNC_PROMPT>`, SHAs filled in). Record each machine's `node -v`, `go version`, Docker availability and, for `Linux`, `key:present`.

## Phase 2: Tracking in GitHub
The 22 issues exist (created 2026-10-21): epics #469, #476, #482, #486; stories and tasks nested as sub-issues. Verify, do not recreate. Then, idempotently:
1. Labels `status:ready`, `status:in-progress`, `status:in-review`, `status:blocked`, `needs-human`, `host:linux`, `host:imac`, `host:z600` already exist from RW; reuse them. Create `wave:wm-1` … `wave:wm-7` only if you want wave labels; otherwise track waves in the tracker table.
2. **"WM tracker" issue** `[WM] Implementation tracker` (search by title first; pin it): a table Task | Issue | Wave | Host | Agent | Session | Branch/PR | Status. Update it on every status change. Mirror the Status column into `docs/backlog/webmcp/backlog/README.md` (the block between `<!-- status:begin -->` and `<!-- status:end -->`) and the `**Status:**` line of each task file in a small `docs(backlog):` commit on `dev` after each merge.

## Phase 3: Execution waves
Dependencies come from the task files. A task starts when all its dependencies are merged into `dev`, not when its wave number comes up.

| Wave | Tasks | Waits for | Host · agent |
|---|---|---|---|
| 1 | WM-1.1.1 (shim), WM-3.1.1 (record schema + PROTOCOL.md), WM-4.1.1 (chart primitive) | nothing | WM-1.1.1 `IMac` · claude-code; WM-3.1.1 `Z600` · codex; WM-4.1.1 `IMac` · codex |
| 2 | WM-1.2.1 (board tools), WM-1.2.2 (settings tools), WM-1.2.3 (chat tools) | WM-1.1.1 | `IMac` · claude-code, `Z600` · claude-code, `Linux` · claude-code (three parallel; renderer work, Electron regression checked on `IMac` at review) |
| 3 | WM-2.1.1 (catalog + assert layer) | WM-1.2.1, WM-1.2.2, WM-1.2.3 | `Z600` · codex |
| 4 | WM-2.2.1 (scripted + Playwright drivers, bench spec) | WM-2.1.1, WM-3.1.1 | `Z600` · codex |
| 5 | WM-2.2.2 (JEV driver) | WM-2.2.1 | **`Linux`** · claude-code (needs the key for its verification) |
| 6 | WM-3.1.2 (measured runs, committed data); WM-4.1.2 (benchmarks route, may use a fixture until data lands) | WM-3.1.2 ← WM-2.2.2; WM-4.1.2 ← WM-4.1.1 + WM-3.1.1 | WM-3.1.2 **`Linux`** · codex, idle machine, nothing else running there during the run; WM-4.1.2 `IMac` · claude-code |
| 7 | WM-4.1.3 (performance report) | WM-3.1.2, WM-4.1.2 | any · codex |

Notes on placement:
- WM-2.2.2 and WM-3.1.2 **must run on `Linux`**: it is the only host with the key. The worker inherits the environment of the AO daemon on that host; if `[ -n "$JEV_MAF_LAB" ]` is false inside the worker, stop with `needs-human` and ask the user to export it for the daemon (do not ask for the value).
- WM-3.1.2 is a measurement: while it runs, no other worker on `Linux` (`ao session ls --host Linux` must show only that session). Rerun if another session was active during the run.
- WM-4.1.2 may merge before WM-3.1.2 with a fixture in `benchmarks/data/`; WM-3.1.2 then replaces the fixture. The dashboard spec must pass against both.
- Keep the 1 `claude-code` + 1 `codex` slot per machine; the agents in the table are the starting assignment, not a rule.

## Starting a worker
```
ao spawn --host <H> --project ultra-factory \
  --harness <claude-code|codex> \
  --branch wm/<TASK_ID> \
  --issue <ISSUE_NUMBER> \
  --name "<TASK_ID> <short name>" \
  --prompt "<WORKER_PROMPT>"
```

`<WORKER_PROMPT>` (fill in the placeholders):
> You are a worker on task `<TASK_ID>` from the "WebMCP UI testing benchmark" backlog. GitHub issue: #<ISSUE_NUMBER>.
> 1) **Sync before work:** `git fetch origin` then `git merge --ff-only origin/dev`. Your branch `wm/<TASK_ID>` is created from `dev`, so this must pass; otherwise `ao report --stuck`.
> 2) Read `AGENTS.md`, `CLAUDE.md`, `docs/backlog/webmcp/webmcp-report.md` (§1a decisions are final), the story `<STORY_PATH>` and the task `<TASK_PATH>`. For any renderer or UI change also read `DESIGN.md` and use `components/ui/*`.
> 3) **Implement the blueprint in the task file**: the named files, the tool contracts (names, `inputSchema`, results), the tests listed under Subtasks, and nothing else. The `file:line` references were verified on 2026-10-21 against `dev` @ `8a85844fb`; if a reference has moved, find the same symbol and continue. If a decision from §1a looks wrong or the blueprint contradicts the code, stop with `ao report --needs-input "<what>"`; do not redesign.
> 4) Hard rules: no network in tests outside `AO_WEBMCP_BENCH=1`; never print, log, commit or paste the API key (read `TYPESAFE_API_KEY` or `JEV_MAF_LAB` from the environment only); do not modify existing `frontend/e2e/*.spec.ts` beyond what the task names; tools wrap existing handlers and add no business logic; `AGENTS.md` hard rules apply.
> 5) Verify with every command in the task's "Verification" section plus `npm --prefix frontend run typecheck`, `npm --prefix frontend run typecheck:e2e`, `npm --prefix frontend test` for frontend changes and `make ci-check` where the toolchain exists. Do not report success without green output; paste the tail of each command in the PR.
> 6) **English only** for everything you write (docs, comments, commits, PR, issue comments). Conventional Commits. **No `Co-Authored-By`, AI attribution or session links.** No `--no-verify`, no force push.
> 7) Push to `wm/<TASK_ID>` and open a PR: `gh pr create --base dev --head wm/<TASK_ID> --title "<TASK_ID>: <title>" --body "Closes #<ISSUE_NUMBER>"` plus a summary, the checked-off subtasks and the verification output. For UI tasks attach screenshots in both themes and run `ao preview` from inside the session.
> 8) Check off subtasks in issue #<ISSUE_NUMBER> as you complete them.
> 9) Report: `ao report --checkpoint` on significant progress, `ao report --done --pr-created <URL>` at the end, `ao report --needs-input` or `--stuck` when blocked.
> 10) For WM-3.1.2 only: before the run confirm the machine is idle, follow `docs/performance/webmcp/PROTOCOL.md` exactly, keep failed repeats, and commit `measurements.json` and `summary.json` as the task says; paste the printed Markdown table in the PR.

After starting: issue → `status:in-progress` + `host:<h>`, tracker row with the session id and agent.

## Review checklist (in addition to the Railway "Review and merge" steps)
- The diff touches only the files the task names (plus tests and i18n catalogs). Extra `frontend/e2e/*.spec.ts` edits → send back.
- Every tool in the PR appears in the task's contract with the same `name` and `inputSchema`; results are JSON, no DOM nodes.
- `__aoWebMcp` is unreachable in a production build (WM-1.1.1 acceptance).
- For WM-2.x: original specs pass unchanged; the bench spec is skipped without `AO_WEBMCP_BENCH=1`.
- For WM-2.2.2 and WM-3.1.2: `git grep -I "$JEV_MAF_LAB"` on the PR head on `Linux` prints nothing; records contain `jev.model` and no headers.
- For WM-4.1.x: `DESIGN.md` compliance, both themes, 390 px width, table twin per chart, `ao preview` screenshot in the PR.
- For WM-4.1.3: every number traces to `summary.json`.
- Merge with `gh pr merge <PR> --squash --match-head-commit <SHA> --delete-branch --subject "<PR title> (#<PR>)" --body "<English summary>"`, then close the task issue manually (`Closes` only fires on `main`), update the tracker and the backlog README status block, and kill the worker session per the Railway rule 7.

## End of initiative
When WM-4.1.3 is merged: post the results table from `docs/performance/webmcp/README.md` in the WM tracker and to the user, and open a **draft** PR `dev → main` titled `feat: WebMCP UI testing benchmark (WM-1…WM-4)` with a description by epic. Do not merge it.

## Start now
Run Phase 0, then Phase 1, then Phase 2. Show the user: the sync result (SHAs, `node -v` per machine, `key:present` on `Linux`), the tracker link, and the wave-1 plan by machine. Then start wave 1 (three workers: WM-1.1.1, WM-3.1.1, WM-4.1.1).
