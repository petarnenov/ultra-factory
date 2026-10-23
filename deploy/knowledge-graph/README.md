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

On every machine whose daemon runs workers or that runs `geo.architect`
(including the orchestrator host), write `~/.ao/knowledge-graph.json`
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

### Read-only access

The deployed image is Neo4j Community (`neo4j:2026.09.0-community`). Community
does not provide role-based privileges for a second user, so do not create a
second Neo4j account and assume it is read-only. The read-only endpoint for
this deployment must be a separately operated HTTP Query API proxy that
authenticates its own credential, forwards only read statements, and rejects
write statements and all other Neo4j endpoints. It must not forward write
requests to the primary Neo4j listener. A supported upgrade to an edition with
database RBAC can replace this proxy only after its grants are verified.

Once that proxy is provisioned and its write-denial check passes, add its
endpoint and credential beside the primary write credential in each daemon's
private `~/.ao/knowledge-graph.json` (mode 600):

```json
{
  "url": "http://<tailnet-ip>:7474",
  "user": "neo4j",
  "password": "<primary password>",
  "database": "neo4j",
  "readOnly": {
    "url": "https://<read-only-proxy>:<port>",
    "user": "<proxy user>",
    "password": "<proxy password>"
  }
}
```

The daemon uses `readOnly` for prior-context, search, duplicate lookup, and
embedding-discovery reads. Ingest and explicit graph writes continue to use
the primary fields. Until the proxy is provisioned, no genuinely read-only
credential exists for this Community deployment; `ao knowledge` falls back to
the primary connection when `readOnly` is absent. Keep review queries on the
read-only proxy endpoint and credential after provisioning.

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

Use the read-only proxy's supported Browser or Query API interface and the
`readOnly` credential for inspection. Do not run review queries against the
primary Community endpoint. Useful queries:

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
// Rating totals are populated as workers use the #560 rating command.
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
#560 and are populated as workers rate lessons and the operator retires them.
Wait for rollout to reach every worker host before treating these counts as
complete. The separate wrong-rated query uses the same rating data:

```cypher
// Lessons workers rated wrong after #560 rollout.
MATCH (l:Lesson)
WHERE coalesce(l.wrong, 0) > 0
RETURN l.project, l.id, l.kind, l.text, l.wrong
ORDER BY l.wrong DESC LIMIT 50;
```

Review retired lessons and their operator-recorded reasons after #560 rollout:

```cypher
// Requires operator retirements from #560.
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
  AND a.retiredAt IS NULL AND b.retiredAt IS NULL
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
  AND l.retiredAt IS NULL
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
The rating and retirement fields from #560 are populated as workers and the
operator use those operations. Wait until rollout reaches every worker host
for complete counts, then run each query and record the results in an issue
comment. These review queries are read-only; retiring a lesson remains an
operator-only write.

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

Architecture consultation indexing and host catch-up are described in
[the consultation runbook](consultations.md).
