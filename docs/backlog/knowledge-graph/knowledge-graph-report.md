# Knowledge graph usefulness: new features and reworked issues

**Date:** 2026-10-23. **Base:** `dev` @ `bb9c8503c` (daemons run `5d16ca700`).
**ADRs:** [0012](../../adr/0012-central-knowledge-graph.md),
[0013](../../adr/0013-semantic-search-with-local-embeddings.md).

## Goal

Make the central knowledge graph useful in two cases:

1. **Rework:** a worker picks up an issue that an earlier worker attempted.
2. **New feature:** a worker starts a task that no earlier task matches by
   issue.

Keep the per-task prompt cost small and avoid any model in the recording path.

## 1. Conclusions

- **The graph is young.** On 2026-10-23 it held 12 tasks, 10 lessons and one
  project (`ultra-factory`). Lessons were served 5 times; two have
  `helped=1` and none has `hurt`. Ranking does not mean anything yet.
- **The lessons describe the environment, not the design.** Examples:
  `make ci-check` needs `npm ci`, branch naming, and "issue status lives in
  the comments". They save a worker minutes but do not show how a feature is
  built.
- **"Earlier attempts on this issue" is the strongest section.** It matches
  exactly by issue key. It lacks the PR, the branch and why the attempt
  failed, which are what a rework needs.
- **New features get little.** With no issue match, only lessons and tasks
  above `minScore` 0.70 and within 0.04 of the best match are shown. Often
  that is nothing.
- **Usefulness scoring is coarse.** Every lesson served to a merged task gets
  `helped+1`, whether or not it was relevant (`graph.go` `recordTaskCypher`).
- **Project isolation holds.** Lessons, tasks and attempts are filtered by
  project id or by `project/issue` key (`graph.go` `lessonsCypher`,
  `tasksCypher`, `attemptsCypher`; `service.go` `Search`). Caveats:
  - the id is chosen per host, so two repositories registered under the
    same id would share lessons;
  - the vector query takes the 100 nearest nodes across all projects and
    filters by project after that (`vectorOversample`), which costs recall
    for a small project as others grow.
- **Logic changes need a daemon rollout on every host.** Queries, ranking,
  prompt formatting, recording and the `ao knowledge` skill text all live in
  the daemon binary. `make kg-deploy` updates only the Neo4j and Ollama stack
  on Linux.

## 2. What exists in the code today

- The spawn query text is the full task prompt plus issue context:
  `service/session/knowledge_context.go` `withKnowledgeContext`
  (`cfg.Prompt + "\n" + cfg.IssueContext`).
- Task facts already stored (`knowledgegraph/graph.go` `RecordTask`): title,
  issue, outcome, harness, model, effort, stages, `prs` (URLs),
  `elapsedSeconds`, input and output tokens, plus the worker report text as
  `summary` and `reports`.
- `attemptsCypher` returns title, outcome, summary and up to 5 lessons. It
  does not return `t.prs`.
- The daemon's SQLite already holds a PR's failure evidence. All of it is
  reachable through the store:
  - `ListChecks(prURL)` returns `domain.PullRequestCheck` with `Status` and
    `LogTail`.
  - `ListPRComments(prURL)` returns `domain.PullRequestComment` with
    `Author`, `File`, `Body`, `Resolved` and `IsBot`.
  - `ListPRReviews(prURL)` lists the PR's reviews.
  - `domain.Session.PRs []PRFacts` carries `SourceBranch`, `TargetBranch`,
    `ExternalChangesRequested` and `ChangedFiles` (a count only).
- No code records which files a task changed. `domain.Session` has
  `WorkspacePath` and `Branch`.
- Tooling causes behind the current lessons:
  - **`ci-check` without `npm ci`:** already fixed. `scripts/ci-check.mjs`
    prints the `npm ci --prefix frontend --ignore-scripts` fix
    (lines 257–263).
  - **Branch naming:** already covered. The worker prompt explains hyphen
    siblings for bare `ao/<id>` workspace branches
    (`session_manager/prompt.go` `workerMultiPRPrompt`). Normal workers get
    `ao/<id>/root` (`session_manager/manager.go` `defaultSessionBranch`).
  - **CLI tests under a session env: still broken.**
    `AO_SESSION_ID=x AO_PROJECT_ID=y go test ./internal/cli/` fails
    `TestSpawnCommand_MissingProjectContext` and
    `TestSpawnResolvesProjectFromCWD`.
- Task nodes without a title are in-flight workers, not junk.
  `MarkServed` creates the node at spawn (`graph.go` `MarkServed`), and the
  title arrives with the first report.

## 3. Options considered

| Option | Decision |
|---|---|
| Deterministic facts from data the daemon already has: PR, branch, CI, human review, changed files | **Chosen.** No model in the recording path; nothing new to fetch over the network. |
| LLM summaries of each task and of each failure | Rejected. The daemon has no model, and a summary can be wrong without anyone noticing. |
| A `Module`/`Decision` ontology | Rejected for now. It needs a new schema and an extractor, and git history and ADRs already hold most of it. |
| Promoting useful lessons into `AGENTS.md` | Rejected as a default. `AGENTS.md` is about 25 KB and loads into every session. See §4 for the triage rule. |
| Hybrid vector + full-text search, daemon version handshake, repository-based project identity | Deferred until data shows a need. |

## 4. Principles

1. **Precision over volume.** The section keeps its 6000-character cap
   (`format.go` `maxSectionText`), and every new field is clipped.
2. **Deterministic sources only:** daemon facts, git, CI and human review.
3. **A recurring lesson triages in this order:**
   - fix the tool, then retire the lesson;
   - keep it in the graph if only part of the code needs it, or move it to
     a subdirectory `CLAUDE.md` if it must always be visible there;
   - add it to `AGENTS.md` only when every task in the repository needs it.
4. **External text is data.** CI log tails and review comments are clipped
   and stay behind `SectionTrustBoundary`.
5. **One rollout.** Daemon-side tasks merge to `dev` first and roll out to
   all hosts together.

## 5. Plan and estimate

| Epic | Content | Estimate |
|---|---|---:|
| KG-0 | Fix the tooling cause still open behind a recorded lesson | 0.25 pd |
| KG-1 | Rework context: PR, branch and failure facts in earlier attempts | 1.5 pd |
| KG-2 | New-feature context: changed files on similar tasks | 1 pd |
| KG-3 | Lesson quality: `ao knowledge rate`, retired lessons | 1 pd |
| KG-4 | Process: lesson review runbook, effect measurement at ~50 tasks | 0.75 pd |

Total: 4.5 pd. Order and issue numbers are in [backlog/README.md](backlog/README.md).

## 6. Risks

- **The worktree may be gone when the task ends.** Changed files are
  captured on worker reports, while the worktree exists, and kept after
  that.
- **Prompt noise.** More facts per attempt could crowd out lessons. Facts
  are capped per attempt and the section cap stays.
- **Prompt injection through review or CI text.** Clipped, framed as data,
  and never placed above project rules.
- **Too little data for measurement.** KG-4.2 is gated on about 50
  recorded tasks with an outcome.
