# Backlog: Knowledge graph usefulness (key `KG`)

Source: [`../knowledge-graph-report.md`](../knowledge-graph-report.md).
ADRs: [0012](../../../adr/0012-central-knowledge-graph.md),
[0013](../../../adr/0013-semantic-search-with-local-embeddings.md).

## Structure

```text
backlog/
  KG-1-rework-context/                 epic -> EPIC.md
    KG-1.1-attempt-facts/              story -> STORY.md
      KG-1.1.1-pr-and-branch-in-attempts.md   task
```

- **Epic** = a delivery phase. **Story** = an operator-visible capability.
  **Task** = one reviewable implementation unit. Subtasks are checklists.
- Every task has Goal, Context, Subtasks, Acceptance criteria, Verification,
  Estimate, Order, Dependencies and Next.
- Estimates are person-days. Status icons follow the
  [RW backlog legend](../../railway/backlog/README.md#status).

## Execution order

Tasks run strictly in this order; each task names the one that follows it.

| Order | Task | Priority | Why here |
|---:|---|---|---|
| 1 | KG-0.1.1 (#550) CLI tests isolated from the session env | medium | Smallest; removes a recurring false failure every worker hits |
| 2 | KG-1.1.1 (#553) PR URL and branch in earlier attempts | medium | Cheapest high-value rework fact; data already stored |
| 3 | KG-1.1.2 (#554) CI and human review failure facts | medium | Tells a rework what blocked the last attempt |
| 4 | KG-2.1.1 (#557) Changed files on similar tasks | medium | The new-feature gain; reuses KG-1.1.1's PR field |
| 5 | KG-3.1.1 (#560) `ao knowledge rate` and retired lessons | medium | Keeps precision as KG-1/KG-2 add facts |
| — | **Shared rollout of tasks 2–5** | — | One daemon rollout to every host (see below) |
| 6 | KG-3.1.2 (#561) Retire lessons resolved in tooling | low | Needs `retire` live and KG-0.1.1 merged |
| 7 | KG-4.1.1 (#564) Lesson review runbook | low | Needs ratings and retire to exist |
| 8 | KG-4.2.1 (#566) Measurement note | low | Blocked until ~50 recorded tasks |

Total: 4.5 pd. Tasks 2–5 touch the same package (`backend/internal/knowledgegraph`);
run them one at a time, never in parallel, to avoid conflicts in `graph.go`
and `format.go`.

## Shared rollout

Tasks 2–5 change the daemon binary. After all four are merged to `dev`, roll
the new daemon to every host that records workers, then verify:

- each host logs `knowledge graph: enabled … semantic=true`;
- one probe worker per host shows the new attempt and similar-task fields;
- the Electron daemon on the Mac needs "Restart daemon".

`make kg-deploy` is not needed: no Neo4j schema or model change. If a hub is
running again, it needs a new `worker-v*` tag and `AO_WORKER_TAG`.

## Out of scope (decided 2026-10-23)

LLM summaries; a `Module`/`Decision` ontology; default promotion of lessons
into `AGENTS.md`; hybrid search; daemon version handshake; repository-based
project identity. Revisit after KG-4.2.1.

Lesson dedup is no longer open: #568 (ADR
[0014](../../../adr/0014-merge-near-identical-lessons.md)) merges a
near-identical lesson of the same project and kind on write into the stored
one, with a `CONFIRMED` edge and a `confirmations` count. It does not merge
lessons stored before it, and `confirmations` does not affect ranking yet.
Time decay is still open (KG-4.2.1).

## GitHub issue tracking

Issues mirror the Epic → Story → Task hierarchy as sub-issues, with labels
`kg`, `kg:epic`, `kg:story`, `kg:task` plus exactly one type and one
`priority:*` label per `docs/issue-policy.md`. Each issue body is the
blueprint file. Branch names use `kg/<task-id>`; PRs go to `dev` with
`Closes #<task issue>`. These issues are not in the priority tracker #501
until the user schedules them.

<!-- status:begin -->
| Item | GitHub issue | Status |
|---|---:|---|
| [KG-0] Epic: Fix the tooling cause behind a recorded lesson | [#548](https://github.com/petarnenov/ultra-factory/issues/548) | ⬜ todo |
| [KG-0.1] Story: CLI tests pass inside a worker session | [#549](https://github.com/petarnenov/ultra-factory/issues/549) | ⬜ todo |
| [KG-0.1.1] Task: Isolate CLI tests from the AO session environment | [#550](https://github.com/petarnenov/ultra-factory/issues/550) | ⬜ todo |
| [KG-1] Epic: Rework context in earlier attempts | [#551](https://github.com/petarnenov/ultra-factory/issues/551) | ⬜ todo |
| [KG-1.1] Story: Earlier attempts carry PR, branch and failure facts | [#552](https://github.com/petarnenov/ultra-factory/issues/552) | ⬜ todo |
| [KG-1.1.1] Task: PR URL and branch in earlier attempts | [#553](https://github.com/petarnenov/ultra-factory/issues/553) | ⬜ todo |
| [KG-1.1.2] Task: CI and human review failure facts for unmerged attempts | [#554](https://github.com/petarnenov/ultra-factory/issues/554) | ⬜ todo |
| [KG-2] Epic: New-feature context from where similar tasks changed code | [#555](https://github.com/petarnenov/ultra-factory/issues/555) | ⬜ todo |
| [KG-2.1] Story: Similar tasks show where they changed code | [#556](https://github.com/petarnenov/ultra-factory/issues/556) | ⬜ todo |
| [KG-2.1.1] Task: Record changed files and show them on similar tasks | [#557](https://github.com/petarnenov/ultra-factory/issues/557) | ⬜ todo |
| [KG-3] Epic: Lesson quality | [#558](https://github.com/petarnenov/ultra-factory/issues/558) | ⬜ todo |
| [KG-3.1] Story: Workers flag wrong lessons; fixed lessons retire | [#559](https://github.com/petarnenov/ultra-factory/issues/559) | ⬜ todo |
| [KG-3.1.1] Task: `ao knowledge rate` and retired lessons | [#560](https://github.com/petarnenov/ultra-factory/issues/560) | ⬜ todo |
| [KG-3.1.2] Task: Retire lessons already resolved in tooling | [#561](https://github.com/petarnenov/ultra-factory/issues/561) | ⬜ todo |
| [KG-4] Epic: Lesson review and effect measurement | [#562](https://github.com/petarnenov/ultra-factory/issues/562) | ⬜ todo |
| [KG-4.1] Story: Lesson review runbook | [#563](https://github.com/petarnenov/ultra-factory/issues/563) | ⬜ todo |
| [KG-4.1.1] Task: Review queries and triage rule in the runbook | [#564](https://github.com/petarnenov/ultra-factory/issues/564) | ⬜ todo |
| [KG-4.2] Story: Effect measurement at about 50 recorded tasks | [#565](https://github.com/petarnenov/ultra-factory/issues/565) | ⬜ todo |
| [KG-4.2.1] Task: Measurement note and decay decision | [#566](https://github.com/petarnenov/ultra-factory/issues/566) | ⛔ blocked |
<!-- status:end -->

## Repo rules every task follows

- Keep SQL, git and HTTP out of `knowledgegraph`; add a small port and wire
  it in `backend/internal/daemon/knowledge_wiring.go`.
- Live tests (`-tags kglive`) must not run the global backfill.
- Keep comments to at most two lines; run `make ci-check` before a PR.
- Keep repository documents and comments in English.
