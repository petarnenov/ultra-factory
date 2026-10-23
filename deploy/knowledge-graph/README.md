# Knowledge graph runbook

The central Neo4j knowledge graph that AO daemons fill with task outcomes and
worker lessons, and read prior context from when a worker spawns. Design:
[ADR 0012](../../docs/adr/0012-central-knowledge-graph.md).

## Deploy or update

```bash
make kg-deploy                                  # default host: Linux on the tailnet
make kg-deploy KG_HOST=petar@100.106.144.10     # another host
```

The script copies `compose.yaml` and `schema.cypher` to `~/.ao/knowledge-graph`
on the host, creates `.env` once (random password, the host's Tailscale IPv4),
starts Neo4j, waits until it is healthy, and applies the schema. Re-running it
keeps the data volume and `.env`.

Ports `7474` (HTTP, Query API and Browser) and `7687` (Bolt) bind to
`127.0.0.1` and the Tailscale address only, and so does the embedding service
on `11435` (Ollama with `KG_EMBED_MODEL`, default `bge-m3`; see
[ADR 0013](../../docs/adr/0013-semantic-search-with-local-embeddings.md)).

## Connect a daemon

On every machine whose daemon runs workers, write `~/.ao/knowledge-graph.json`
with mode 600 and restart the daemon:

```json
{"url": "http://100.121.193.125:7474", "user": "neo4j", "password": "<KG_PASSWORD from .env>", "database": "neo4j",
 "embeddingUrl": "http://100.121.193.125:11435", "embeddingModel": "bge-m3"}
```

Without `embeddingUrl` the daemon uses full-text search. `minScore` (default
`0.70`) sets the vector match floor. `mergeScore` (default `0.94`) is the
score at which a new lesson merges into a stored one of the same kind
(ADR 0014). As with `minScore`, `0` means the default; `1` merges only
identical vectors. `AO_KNOWLEDGE_GRAPH_EMBEDDING_URL`,
`_EMBEDDING_MODEL`, `_MIN_SCORE` and `_MERGE_SCORE` override the file.

The daemon reads `knowledge-graph.json` next to its data dir, so the default
`~/.ao/data` reads `~/.ao/knowledge-graph.json`, an isolated dev daemon
(`~/.ao/dev/data`) reads `~/.ao/dev/knowledge-graph.json`, and test daemons
with scratch data dirs read none and never write to the shared graph.

`AO_KNOWLEDGE_GRAPH_URL`, `_USER`, `_PASSWORD`, `_DATABASE`, `_TIMEOUT` and
`_CONFIG` (another file path) override the file. The daemon log shows
`knowledge graph: enabled` at boot; a file readable by group or others is
refused with a warning.

Check from a worker session:

```bash
ao knowledge search "sqlite migration"
```

`501` means this daemon has no graph configured.

## What is recorded

| Event | Write |
| --- | --- |
| `ao report --done/--stuck/--needs-input` | `Task` upsert with the report text appended to `reports` |
| Session turns terminated | `Task` outcome, elapsed seconds, tokens, stage totals, PR URLs |
| `ao knowledge add` | `Lesson` linked to its `Task` and `Topic`s, with its embedding; a near-identical lesson of the same kind gains `confirmations` and a `CONFIRMED {texts}` edge instead (ADR 0014) |
| Spawn | `RECEIVED` links from the new task to the lessons it was shown |
| After any write, and every 5 minutes | `embedding` on lessons and tasks that lack one |

When a task settles as `merged` its received lessons gain `helped`; `closed`
or `failed` adds `hurt`.

## Inspect

Open `http://<tailnet-ip>:7474` in a browser on the tailnet and sign in as
`neo4j`. Useful queries:

```cypher
// Most useful lessons per project
MATCH (l:Lesson) RETURN l.project, l.kind, l.text, l.helped, l.hurt
ORDER BY l.helped - l.hurt DESC LIMIT 25;

// Recent failed tasks with their last report
MATCH (t:Task) WHERE t.outcome IN ['failed', 'closed']
RETURN t.project, t.title, t.summary, t.updatedAt ORDER BY t.updatedAt DESC LIMIT 25;
```

## Lesson review

Review lessons every two weeks in Neo4j Browser or through the Query API.
Record the review date, findings, decisions, and any follow-up issue in an
issue comment. Run these read-only queries against the graph:

```cypher
// Most-served lessons, with outcome and rating totals.
// helped/hurt are available now; useful/wrong require the #560 rollout.
MATCH (l:Lesson)
OPTIONAL MATCH (:Task)-[r:RECEIVED]->(l)
WITH l, count(r) AS served
RETURN l.project, l.id, l.kind, l.text, served,
       coalesce(l.helped, 0) AS helped, coalesce(l.hurt, 0) AS hurt,
       l.useful AS useful, l.wrong AS wrong,
       l.retiredAt AS retiredAt, l.retiredReason AS retiredReason
ORDER BY served DESC LIMIT 50;
```

The `useful`, `wrong`, `retiredAt`, and `retiredReason` fields were added by
#560. Run queries using these fields only after the all-host rollout. The
separate wrong-rated query is also pending that rollout:

```cypher
// Pending #560 rollout: lessons workers rated wrong.
MATCH (l:Lesson)
WHERE coalesce(l.wrong, 0) > 0
RETURN l.project, l.id, l.kind, l.text, l.wrong
ORDER BY l.wrong DESC LIMIT 50;
```

Review retired lessons and their operator-recorded reasons after rollout:

```cypher
// Pending the all-host rollout of #560.
MATCH (l:Lesson)
WHERE l.retiredAt IS NOT NULL
RETURN l.project, l.id, l.kind, l.text, l.retiredAt, l.retiredReason
ORDER BY l.retiredAt DESC LIMIT 50;
```

Find likely near-duplicates by cosine similarity within one project. Treat
`0.94` as a review threshold, matching the default merge score; this query
needs populated lesson embeddings:

```cypher
MATCH (a:Lesson), (b:Lesson)
WHERE a.project = b.project AND a.id < b.id
  AND a.embedding IS NOT NULL AND b.embedding IS NOT NULL
WITH a, b, vector.similarity.cosine(a.embedding, b.embedding) AS similarity
WHERE similarity >= 0.94
RETURN a.project, similarity, a.id AS firstId, a.text AS firstText,
       b.id AS secondId, b.text AS secondText
ORDER BY similarity DESC LIMIT 50;
```

Find lessons created more than 60 days ago that have never been served:

```cypher
MATCH (l:Lesson)
WHERE l.createdAt < datetime() - duration('P60D')
  AND NOT EXISTS { MATCH (:Task)-[:RECEIVED]->(l) }
RETURN l.project, l.id, l.kind, l.text, l.createdAt
ORDER BY l.createdAt ASC LIMIT 50;
```

Triage findings in this order:

1. If a tooling problem caused the lesson to be wrong or unhelpful, file an
   issue to fix the tool, then retire the lesson.
2. If the lesson applies to one area of the code, keep it in the graph. Move
   it to that directory's `CLAUDE.md` only when it must always be visible in
   that area.
3. If it applies to every task in the repository, add a short rule to
   `AGENTS.md`. This should be rare.

Retiring a lesson writes to the shared graph. Only the operator performs that
write; workers can identify and report candidates but must not retire lessons.
Retirement and rating commands, and live validation of queries that use the
new `useful`, `wrong`, or retirement fields, remain pending until #560 has
rolled out to every host. Run every query and record its result counts in the
issue comment once the rollout is available. Do not treat the pending queries
or final live validation as complete before then.

## Change the embedding model

The vector dimensions follow the model. Set `KG_EMBED_MODEL` in `.env`, update
the dimensions in `schema.cypher`, then:

```cypher
DROP INDEX lesson_embedding IF EXISTS;
DROP INDEX task_embedding IF EXISTS;
```

Run `make kg-deploy`, update `embeddingModel` in every daemon's config and
restart the daemons. Each daemon probes the model's vector size and re-embeds
every node whose vector has another size, in the background.

## Backup and restore

```bash
cd ~/.ao/knowledge-graph
docker compose -p ao-knowledge-graph stop neo4j
docker compose -p ao-knowledge-graph run --rm -v "$PWD/backups:/backups" \
  --entrypoint neo4j-admin neo4j database dump neo4j --to-path=/backups
docker compose -p ao-knowledge-graph start neo4j
```

Restore with `neo4j-admin database load neo4j --from-path=/backups
--overwrite-destination=true` the same way, while Neo4j is stopped.

## Rotate the password

Change it with `ALTER CURRENT USER SET PASSWORD FROM '<old>' TO '<new>'` in
the Browser, update `KG_PASSWORD` in `.env`, and update every daemon's
`~/.ao/knowledge-graph.json`, then restart those daemons.
