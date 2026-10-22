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
`0.70`) sets the vector match floor; `AO_KNOWLEDGE_GRAPH_EMBEDDING_URL`,
`_EMBEDDING_MODEL` and `_MIN_SCORE` override the file.

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
| `ao knowledge add` | `Lesson` linked to its `Task` and `Topic`s |
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

## Change the embedding model

The vector dimensions follow the model. Set `KG_EMBED_MODEL` in `.env`, update
the dimensions in `schema.cypher`, then:

```cypher
DROP INDEX lesson_embedding IF EXISTS;
DROP INDEX task_embedding IF EXISTS;
MATCH (n) WHERE n:Lesson OR n:Task REMOVE n.embedding;
```

Run `make kg-deploy`, update `embeddingModel` in every daemon's config and
restart the daemons; they re-embed every node in the background.

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
