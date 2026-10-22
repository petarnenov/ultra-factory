# 12. Workers learn from earlier tasks through a central knowledge graph

Date: 2026-10-23
Status: Accepted

## Context

Workers on every machine start each task from scratch. A worker that hits the
same migration-number clash, flaky test, or misconfigured host as an earlier
worker has no way to know it was hit before. The facts exist, but they are
scattered: task outcomes in each daemon's SQLite (`taskreport`), worker
reports in the report outbox, and lessons only in transcripts.

We want one store that every daemon writes task outcomes and lessons to, and
every new worker reads prior context from, so the fleet improves with each
task.

## Decision

1. **One Neo4j instance per tailnet.** `deploy/knowledge-graph/compose.yaml`
   runs `neo4j:2026.09.0-community` with named volumes for data and logs on
   one worker machine (today Linux). Its ports bind only to `127.0.0.1` and
   the host's Tailscale address, behind Neo4j's own password. `make
   kg-deploy` installs or updates it over SSH; `schema.cypher` is idempotent
   and runs after every start.
2. **The daemon talks to the graph over Neo4j's HTTP Query API**
   (`backend/internal/knowledgegraph`), so there is no Bolt driver
   dependency. It is configured by `~/.ao/knowledge-graph.json` (mode 600) or
   `AO_KNOWLEDGE_GRAPH_*` variables. Without a URL the feature is off and the
   `/api/v1/knowledge/*` routes answer 501. The daemon adds no listener.
3. **Writes.** Each daemon records its own workers:
   - a `done`, `stuck`, or `needs_input` report upserts the `Task` with the
     report text;
   - a session turning terminated upserts the `Task` with the
     `taskreport` outcome (`merged`, `closed`, `failed`, `unfinished`,
     `no_pr`), elapsed time, tokens, and stage totals;
   - `ao knowledge add` stores a `Lesson` linked to the task and topics.
   Writes run on a bounded background queue and never block reports.
4. **Reads.** When a worker spawns, the daemon asks the graph (3 s budget)
   for earlier attempts on the same issue, ranked lessons, and similar past
   tasks of the project, and appends a "Knowledge Graph" section to the task
   prompt. The section carries a trust boundary like tracker context, plus
   the instruction to record lessons before the final report. A graph error
   only drops the section.
5. **Self-improvement.** Lessons served to a task are linked with
   `RECEIVED`. When that task settles, a merge adds one to the lesson's
   `helped` count and a close or failure adds one to `hurt`; ranking is
   full-text score × (1 + helped) / (1 + hurt).

Graph model: `(:Task)-[:IN_PROJECT]->(:Project)`,
`(:Task)-[:FOR_ISSUE]->(:Issue)`, `(:Task)-[:LEARNED]->(:Lesson)`,
`(:Lesson)-[:ABOUT]->(:Topic)`, `(:Task)-[:RECEIVED]->(:Lesson)`. A task's
key is `<hostId>/<sessionId>`, unique across machines.

## Consequences

- Every machine that runs workers needs the config file; a machine without
  it simply neither records nor serves knowledge.
- The graph password grants full read/write to all projects' knowledge. It
  lives only in `~/.ao/knowledge-graph.json` (600) on each daemon host and
  in `.env` (600) next to the compose file.
- Lessons are worker-authored text. They reach prompts only inside the
  trust-boundary section and never as standing instructions.
- The graph is advisory. Losing it loses accumulated lessons but no AO
  state; back it up with `neo4j-admin database dump` (see the runbook).
