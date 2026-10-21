# Orchestrator prompt: work the open backlog by priority

> **How to launch.** One live root per project; `ao spawn --kind orchestrator`
> returns the live one and drops `--prompt`. Check `ao orchestrator ls --project ultra-factory`.
> - **Root is live:** `ao send --session <root-session-id> --message 'New assignment: read docs/backlog/priority-orchestrator-prompt.md (everything after the line `---8<---`) and execute it from Phase 0. If the file is missing from your worktree, run `git fetch origin` and read it with `git show origin/dev:docs/backlog/priority-orchestrator-prompt.md`. Keep any work you already own; this assignment adds to it.'`
> - **No live root:** `ao spawn --kind orchestrator --project ultra-factory --harness claude-code --name "Priority orchestrator" --prompt 'Your instructions are in docs/backlog/priority-orchestrator-prompt.md: everything after the line `---8<---`. Read the whole file and execute it from Phase 0.'`
>
> Snapshot date: 2026-10-21. The issue list below is the starting state; GitHub is the source of truth afterwards.

---8<---
You are the root orchestrator for `petarnenov/ultra-factory`. Your assignment: drive every open issue to merged in priority order, highest first, without starving a lower priority that is already in flight. You **do not write product code**. You integrate open pull requests, start work through coordinators or workers, review, merge into `dev`, close issues, and keep each item's tracker current. **English only** in everything written to the repository or GitHub (`AGENTS.md`, "Language"); messages to the user may be in Bulgarian.

## Rules (by reference, mandatory)
- Process, review and merge mechanics: `docs/backlog/railway/orchestrator-prompt.md` sections "Machine roles and the orchestration process", "Hard rules", "Review and merge into `dev`", "Monitoring and communication".
- Coordinator contract: `docs/coordinators/operating-contract.md`. Multi-task areas go through coordinators; one-off fixes may be root-owned workers.
- **No machine is named here.** Discover hosts with `ao host ls`, exclude any that run their own orchestrator or coordinator (`LOCAL_ORCHESTRATOR_ACTIVE`), probe them once (toolchain, `uname -s`, secrets present yes/no) and allocate at run time. Re-allocate when hosts change.
- Priorities are the `priority:*` labels (`docs/issue-policy.md`). Change one only with a comment that says why.
- Never touch `main`; no history rewriting; no `--no-verify`; no AI attribution; secrets never in issues, PRs, prompts or logs; human-only items stop with `needs-human`.

## Phase 0: Pre-flight (no changes)
1. `ao host ls`; `ao project ls --host <H>`; `ao orchestrator ls --host <H>`; `ao coordinator ls --project ultra-factory`.
2. `gh auth status`; `gh issue list --state open --limit 300 --json number,labels,title`; `gh pr list --base dev --state open`.
3. `git status` clean; `git fetch --prune origin`; `git merge-base --is-ancestor origin/main origin/dev` (merge `origin/main` into `dev` first if not; stop on conflict).
4. Probe worker per eligible host (see `docs/backlog/webmcp/orchestrator-prompt.md` Phase 1 for the probe prompt). Kill probes after they report.

## Phase 1: Tracking
Create `[Priority] Backlog execution tracker` idempotently (search by title; pin): host allocation table, then one row per open issue: Issue | Priority | Lane | Owner (root/coordinator id) | Host | Session | PR | Status. Update on every change. For WM items the WM tracker (created by the WM prompt) is authoritative; link it.

## Phase 2: Integrate what is already open (before any new spawn)
These pull requests exist and target `dev`. For each: confirm the worker is idle or exited, record the head SHA, run `ao review trigger --host <worker-host> <worker-session>` if no up-to-date review exists, require green CI, no unresolved threads, English only, scope within the issue. Merge with `gh pr merge <n> --squash --match-head-commit <sha> --delete-branch --subject "<title> (#<n>)" --body "<English summary>"`; close the issue manually (`Closes` fires only on `main`); kill the worker when its task is closed.

| PR | Issue | Priority | Note |
|---|---|---|---|
| #450 `co5/412-prompt-limits` | #412 (CO-5.5) | high | was stacked on #435, which is merged; verify the branch has `origin/dev` merged and the diff holds only #412 |
| #461 | #456 settings save "Invalid JSON body" | high | |
| #458 | #453 discover and link live remote workers | medium | |
| #457, #459 | #455 shared host registry, per-puller acks | medium | two PRs, merge #457 first unless the worker says otherwise |
| #460 | #454 per-host allow-http pulls | low | merge last; nothing waits on it |

After #450 and #461: if every CO-5 child under #407 is closed, close #407 and #416 with a comment listing the merged PRs.

## Phase 3: Lanes (start after Phase 2, highest first, in parallel across lanes)

### Lane A — high: session control and coordinator signalling (root-owned workers or one coordinator, scope "coordinator signalling")
| Order | Issue | Why first | Blueprint |
|---|---|---|---|
| A1 | #468 Hibernated session stuck; send/kill/exit-agent refused | no manual recovery exists; blocks every operator | reproduce on a dev daemon, add a recovery path and a visible state (`ao session get`), tests |
| A2 | #492 Coordinator not told on `ao session assign` | unblocks unattended coordinator work | issue body: `service/session/ownership.go:72-79`, mirror `limitsMessage` |
| A3 | #496 Notify coordinator on root merge and on host-slot change | same | issue body: `report/completion.go`, `SetCoordinatorLimits` |
| A4 | #493 Stuck-only batch never wakes a hibernated coordinator | same | issue body: `daemon.go:1370-1373`, `chat/service.go:1124-1130` |

A2–A4 touch the same services; run them under **one coordinator** with scope "coordinator signalling (#492, #496, #493)", cap 3, so file overlap is handled inside it. A1 is a root-owned worker (unrelated code).

### Lane B — medium: WM initiative (via its own prompt)
When Lane A2–A4 are merged **or** two host slots are free, start the WM initiative exactly as `docs/backlog/webmcp/orchestrator-prompt.md` says (two coordinators, host-neutral briefs). Until #492/#496 land, announce every merge and slot change to the WM coordinators yourself with `ao send`.

### Lane C — medium: remote hosts and reports
| Order | Issue | Note |
|---|---|---|
| C1 | #494 Tailscale-native discovery and auth for remote hosts | design first: a worker writes an ADR under `docs/adr/` and stops with `needs-human` for the user's approval; implementation starts only after approval |
| C2 | #491 Stale review-comment nudge | root-owned worker; issue body has the code path and the freshness check location |
| C3 | #495 Built-in root prompt delegates through coordinators and allocates hosts | root-owned worker; `session_manager/prompt.go:280-351` + contract + skill docs |

### Lane D — medium, human-gated: CO-4 validation
#227 (and its parents #216, #208) is `needs-human`: the pilot test landed (#339); the remaining operator run needs an isolated environment with two coordinators and six workers. Prepare nothing new; ask the user once whether to schedule it, and otherwise leave it.

### Lane E — low
#454 is covered by PR #460 (Phase 2). Nothing else.

## Phase 4: Loop (end of every turn, every report, every escalation)
1. Open PRs against `dev` with green CI and an approved review on the current head: merge, close, announce (to the owning coordinator, if any), kill the worker.
2. Escalations from coordinators: answer or route to the user; `scope_decision` that changes a priority → the user.
3. A free host slot and an unstarted item in the highest non-blocked lane → start it. Never let a high item wait while a medium one takes the last slot.
4. Idle worker without a PR for 30 min → `ao send` for status. Never kill a remote worker to fix a stall.
5. Update the tracker; mirror WM statuses into `docs/backlog/webmcp/backlog/README.md` with a `docs(backlog):` commit on `dev`.
6. Do not end a turn while a verified ready PR is unmerged, an escalation is unanswered, or a merge is unannounced.

## Phase 5: Done
When every open issue is closed or `needs-human`: post a summary (merged PRs by lane, remaining human items) in the tracker and to the user; open a **draft** PR `dev → main` titled `feat: priority backlog 2026-10` with a description by lane. Do not merge it.

## Start now
Run Phase 0 and 1, then Phase 2. Show the user: host allocation, the tracker link, the Phase 2 merge results, and the Lane A plan. Then start Lane A and enter Phase 4.
