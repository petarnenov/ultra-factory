# 13. Knowledge graph search uses local embeddings

Date: 2026-10-23
Status: Accepted

## Context

[ADR 0012](0012-central-knowledge-graph.md) matched prior context with
Neo4j full-text search, which needs shared words. Workers describe the same
problem in different words ("schema change" vs "migration", "agent stops
right after launch" vs "AppArmor blocks bwrap"), and tasks may be written in
Bulgarian while lessons are in English, so useful lessons were missed.

## Decision

1. **A local embedding model runs next to the graph.** The knowledge-graph
   Compose stack adds `ollama/ollama:0.34.2` with a persistent model volume,
   bound to `127.0.0.1` and the Tailscale address on port `11435` (a system
   Ollama may already own `11434`). `make kg-deploy` pulls the model named
   by `KG_EMBED_MODEL`. Nothing leaves the tailnet and nothing is billed.
2. **The model is `bge-m3` (1024 dimensions).** On a set of AO lessons with
   paraphrased queries that share no keywords it ranked the right lesson
   first 6/6 times, including a Bulgarian query; `nomic-embed-text`,
   `mxbai-embed-large`, `embeddinggemma` and `qwen3-embedding:0.6b` scored
   4-5/6. One query costs about 200 ms on the Linux CPU.
3. **Neo4j vector indexes** (`lesson_embedding`, `task_embedding`, cosine,
   1024 dimensions) replace the full-text indexes for lessons and similar
   tasks when `embeddingUrl` is configured. A lesson is embedded as its
   text; a task as its title and latest report, re-embedded when a new
   report arrives.
4. **Selection.** Matches below `minScore` (default 0.70, Neo4j's
   `(1 + cos) / 2`, so cos 0.40) are dropped, and so is every match more
   than 0.04 below the best one in its section: AO lessons share one
   domain, so a fixed floor alone is not selective. Outcome weighting
   (helped / hurt) still applies to the ranking.
5. **Embedding is asynchronous.** Daemons embed new or changed nodes in the
   background after each write and every five minutes, which also
   backfills nodes written before this change or by another host. If the
   embedder fails at query time the daemon falls back to full-text search.

## Consequences

- Changing the model changes the vector dimensions: drop both vector
  indexes, update `schema.cypher` and redeploy. Daemons probe the model's
  vector size and re-embed every node whose vector has another size.
- The Ollama port is a second service on the tailnet behind no password;
  it only computes embeddings and holds no AO data.
- A node is invisible to vector search until it is embedded, normally
  within a second of the write.
