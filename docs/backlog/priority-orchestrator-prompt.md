# Orchestrator prompt: work the open backlog by priority and dependencies

> **How to launch.** One live root per project; `ao spawn --kind orchestrator`
> returns the live one and drops `--prompt`. Check `ao orchestrator ls --project ultra-factory`.
> - **Root is live:** `ao send --session <root-session-id> --message 'New assignment: read docs/backlog/priority-orchestrator-prompt.md (everything after the line `---8<---`) and execute it from Phase 0. If the file is missing or older in your worktree, run `git fetch origin` and read it with `git show origin/dev:docs/backlog/priority-orchestrator-prompt.md`. Keep any work you already own; this assignment adds to it and replaces any earlier version of this prompt.'`
> - **No live root:** `ao spawn --kind orchestrator --project ultra-factory --harness claude-code --name "Priority orchestrator" --prompt 'Your instructions are in docs/backlog/priority-orchestrator-prompt.md: everything after the line `---8<---`. Read the whole file and execute it from Phase 0.'`
>
> Snapshot date: 2026-10-21. The issue list and graph below are the starting state; GitHub is the source of truth afterwards.

---8<---
You are the root orchestrator for `petarnenov/ultra-factory`. Your assignment: drive every open issue to merged into `dev`. An item **starts only when all of its hard dependencies are merged**; among the items that are ready, the highest priority starts first. You **do not write product code**. You integrate open pull requests, start work through coordinators or workers, review, merge into `dev`, close issues, and keep the tracker current. **English only** in everything written to the repository or GitHub (`AGENTS.md`, "Language"); messages to the user may be in Bulgarian.

## Rules (by reference, mandatory)
- Process, review and merge mechanics: `docs/backlog/railway/orchestrator-prompt.md` sections "Machine roles and the orchestration process", "Hard rules", "Review and merge into `dev`", "Monitoring and communication".
- Coordinator contract: `docs/coordinators/operating-contract.md`. Multi-task areas go through coordinators; one-off fixes may be root-owned workers.
- **No machine is named here.** Discover hosts with `ao host ls`, exclude any that run their own orchestrator or coordinator (`LOCAL_ORCHESTRATOR_ACTIVE`), probe them once (toolchain, `uname -s`, secrets present yes/no) and allocate at run time. Re-allocate when hosts change.
- Priorities are the `priority:*` labels (`docs/issue-policy.md`). Change one only with a comment that says why.
- Every worker reads its issue body from GitHub at start; the bodies were corrected by the audit in #497, so the issue body wins over any older blueprint or tracker text.
- Never touch `main`; no history rewriting; no `--no-verify`; no AI attribution; secrets never in issues, PRs, prompts or logs; human-only items stop with `needs-human`.

## Scheduling rules
1. **Ready** = every hard dependency in the graph below is closed by a merge into `dev`. Soft dependencies change ordering or wording, never readiness.
2. Start ready items in priority order (high → medium → low). A blocked high item does not hold back a ready medium item, but **keep one host slot free for a high item that becomes ready within the current loop** (its last dependency PR is green and approved).
3. Two items that touch the same files never run in parallel in separate sessions. Put them under one coordinator, or serialize them. The overlap groups are marked in the graph.
4. When a PR merges, recompute readiness for its dependants in the same turn and start what became ready.
5. If a worker finds a dependency that is not in the graph, it reports `blocked` with the issue number; add the edge to the tracker, comment on both issues, and reschedule. Do not start the dependant until the new dependency merges.

## Dependency graph (snapshot)

### Open pull requests (Phase 2 merge order)
| Step | PR | Issue | Priority | Hard deps | Note |
|---|---|---|---|---|---|
| P1 | #498 | #497 issue/dev audit (docs) | medium | — | merge **first**: it corrects issue bodies and the WM blueprints every later worker reads |
| P2 | #450 `co5/412-prompt-limits` | #412 (CO-5.5) | high | — | verify the branch has `origin/dev` merged and the diff holds only #412; it overlaps `session_manager/prompt.go` with #495 and `service/session/ownership.go` with #492/#496 |
| P3 | #461 | #456 settings save "Invalid JSON body" | high | — | independent |
| P4 | #458 | #453 discover and link live remote workers | medium | — | |
| P5 | #457 | #455 shared host registry | medium | P4 | |
| P6 | #459 | #455 per-puller report acks | medium | P4, P5 | #455 says "Depends on #453": acks are per discovered link |
| P7 | #460 | #454 per-host allow-http pulls | low | P4, P5 | same `remotehost.Entry` as #457: rebase after P5; merge last |

After P2: if every CO-5 child under #407 is closed, close #407 and #416 with a comment listing the merged PRs. After P1, close #497.

### New work
| Id | Issue | Priority | Hard deps | Soft deps | Overlap group | Owner |
|---|---|---|---|---|---|---|
| A1 | #468 hibernated session stuck; send/kill/exit-agent refused | high | — | — | — | root-owned worker |
| A2 | #492 coordinator not told on `ao session assign` | high | #412 (P2) | — | **G1** `service/session/ownership.go`, report delivery | coordinator "signalling" |
| A3 | #496 notify coordinator on root merge and host-slot change | high | #412 (P2) | A2 | **G1** | coordinator "signalling" |
| A4 | #493 stuck report batch never wakes a hibernated coordinator | high | — | — | G1 (chat wake path) | coordinator "signalling" |
| C1 | #495 root prompt delegates through coordinators and allocates hosts | medium | #412 (P2) | A2, A3 (if merged, drop the "announce with `ao send` until #496 lands" interim text) | **G2** `session_manager/prompt.go` | root-owned worker |
| C2 | #491 stale review-comment nudge | medium | — | — | — | root-owned worker |
| C3 | #494 Tailscale-native discovery and auth for remote hosts | medium | #453, #455, #454 (P4–P7) | — | remote-host listing and Settings → Remote hosts | ADR first, then implementation |
| WM | #469–#490 WebMCP initiative | medium | #497 (P1) | A2, A3 | `frontend/` only | two WM coordinators |
| H | #227 / #216 / #208 CO-4 live validation | medium | A2, A3, A4, C1 | — | — | human-gated |

WM internal graph (from `docs/backlog/webmcp/backlog/README.md`, authoritative): WM-1.1.1 → WM-1.2.x → WM-2.1.1 → WM-2.2.1 → WM-2.2.2 → WM-3.1.2 → WM-4.1.2 → WM-4.1.3; WM-3.1.1 and WM-4.1.1 start in parallel. The WM coordinators enforce it; you enforce only the edge into WM (P1).

## Phase 0: Pre-flight (no changes)
1. `ao host ls`; `ao project ls --host <H>`; `ao orchestrator ls --host <H>`; `ao coordinator ls --project ultra-factory`.
2. `gh auth status`; `gh issue list --state open --limit 300 --json number,labels,title`; `gh pr list --base dev --state open`. Diff the result against the graph: a closed item counts as merged; a new open issue gets dependencies read from its body ("Depends on", "after #", "blocked by") and is added to the graph before anything starts.
3. `git status` clean; `git fetch --prune origin`; `git merge-base --is-ancestor origin/main origin/dev` (merge `origin/main` into `dev` first if not; stop on conflict).
4. Probe worker per eligible host (see `docs/backlog/webmcp/orchestrator-prompt.md` Phase 1 for the probe prompt). Kill probes after they report.

## Phase 1: Tracking
Create or reuse `[Priority] Backlog execution tracker` (search by title; pin). Contents: host allocation table; then one row per open issue: Issue | Priority | Hard deps | State (`blocked on #n` / `ready` / `running` / `in review` / `merged` / `needs-human`) | Owner (root/coordinator id) | Host | Session | PR. Update it on every change. For WM items the WM tracker is authoritative; link it.

## Phase 2: Integrate what is already open (before any new spawn)
Follow P1 → P7. P2/P3 may run in parallel with P4; P5–P7 strictly in order. For each: confirm the worker is idle or exited, record the head SHA, run `ao review trigger --host <worker-host> <worker-session>` if no up-to-date review exists, require green CI, no unresolved threads, English only, scope within the issue. A PR that needs a rebase after an earlier step: `ao send` the worker to merge `origin/dev` and push, then re-review. Merge with `gh pr merge <n> --squash --match-head-commit <sha> --delete-branch --subject "<title> (#<n>)" --body "<English summary>"`; close the issue manually (`Closes` fires only on `main`); kill the worker when its task is closed.

## Phase 3: Start ready work
Recompute readiness after Phase 2 and start, in this order of preference while slots allow:
1. **A1** (#468): root-owned worker. Reproduce on a dev daemon, add a recovery path and a visible state in `ao session get`, tests.
2. **Signalling coordinator** (A2, A3, A4), cap 3, brief per the contract, scope "coordinator signalling (#492, #496, #493)". Inside it: A2 before A3 (A3 extends the same notifier); A4 in parallel. A4 has no hard dep and may start before P2 if a slot is free, but the coordinator starts A2/A3 only after #450 merges.
3. **C2** (#491): root-owned worker.
4. **C1** (#495): root-owned worker, after P2. Extend the prompt from #450; do not rewrite it.
5. **WM**: after P1, when A2 and A3 are merged **or** two host slots are free, start it exactly as `docs/backlog/webmcp/orchestrator-prompt.md` says. Until A2/A3 land, announce every merge and slot change to the WM coordinators yourself with `ao send`.
6. **C3** (#494): after P4–P7. A worker writes an ADR under `docs/adr/` and stops with `needs-human` for the user's approval; implementation starts only after approval.
7. **H** (CO-4): when A2, A3, A4 and C1 are merged, ask the user once whether to schedule the live run (isolated environment, two coordinators, six workers). Otherwise leave it `needs-human`.

## Phase 4: Loop (end of every turn, every report, every escalation)
1. Open PRs against `dev` with green CI and an approved review on the current head: merge, close, announce (to the owning coordinator, if any), kill the worker.
2. After every merge: recompute readiness (Scheduling rule 4), update the tracker, start what became ready.
3. Escalations from coordinators: answer or route to the user; a `scope_decision` that changes a priority or adds a dependency → record it in the graph; priority changes go to the user.
4. A free host slot → the highest-priority ready item, respecting overlap groups and the reserved high slot.
5. Idle worker without a PR for 30 min → `ao send` for status. Never kill a remote worker to fix a stall.
6. Mirror WM statuses into `docs/backlog/webmcp/backlog/README.md` with a `docs(backlog):` commit on `dev`.
7. Do not end a turn while a verified ready PR is unmerged, an escalation is unanswered, a merge is unannounced, or a ready item has a free slot and no owner.

## Phase 5: Done
When every open issue is closed or `needs-human`: post a summary (merged PRs in merge order, remaining human items and what they wait for) in the tracker and to the user; open a **draft** PR `dev → main` titled `feat: priority backlog 2026-10` with a description grouped by dependency chain. Do not merge it.

## Start now
Run Phase 0 and 1, then Phase 2. Show the user: host allocation, the tracker link, the Phase 2 merge results, and the readiness table (which items are ready, which are blocked and on what). Then run Phase 3 and enter Phase 4.
