# Design

## Context

The consultation skill writes an immutable, structured archive under `~/.ao/architecture-consults/archive/<UTC date>/<id>/`. It has `request.md`, `state.json`, independent analyses, critiques, versioned proposals, ballots, `final.md`, and per-consul collected reports. Some consultations may be incomplete. AO already has an optional central Neo4j knowledge graph through its daemon; a graph is suitable for consultation, round, consul, artifact, and decision relationships. A frontend cannot directly read another host's files, and it must not receive arbitrary file paths.

## Goals / Non-Goals

**Goals:** Display the exact recorded content and provenance; keep the consultation navigable across hosts; survive restart and graph downtime; avoid duplicate records on retry; preserve honest process and decision status; keep daemon and UI boundaries; follow the OpenSpec change lifecycle.

**Non-Goals:** Run new consultations from this page, edit or ratify decisions, import CLI logs or credentials, replace the skill's local archive during this change, introduce a new network listener, or alter the three-consul voting rules.

## Decisions

### D1: Local archive is evidence; Neo4j is the query index and retained copy

The caller host keeps the skill-required archive. Each AO host with Neo4j configured scans only its own `~/.ao/architecture-consults/archive` tree at daemon startup and periodically thereafter. It validates the date/ID layout, manifest and file allowlist, bounds file sizes, and hashes the bytes. It upserts a consultation keyed by `(archive_host_id, consultation_id)` and permitted artifacts keyed by their relative path. It stores original Markdown as data, a digest, and structured phase/status metadata. A change in bytes updates that artifact and scan metadata; missing files are recorded as missing, not deleted without an explicit verified archive change. Ingestion failures leave files intact and are retried. The scanner does not follow symlinks outside the archive tree. Logs and absolute local paths stay out of Neo4j and API responses.

Alternatives: Graph-only write at skill execution would make failure of Neo4j block archiving and require changing the skill; a new SQLite store would fragment cross-host browsing and duplicate AO's existing graph infrastructure. The retained archive plus Neo4j index preserves the evidence and supports central reads.

### D2: Model rounds explicitly

Map `independent/` to round 1 analysis, `critique/` to mutual critique, and each immutable `proposal-N.md` plus `ballots/round-N/` to a ratification round. Expose the exact observed number of ratification rounds and a separate count of phase rounds, so the page never implies a ballot round existed when only analysis or critique was completed. Show per-consul original artifact content, expected-but-missing artifacts, and ballot stance only when explicitly present and parseable. Preserve `state.json` escalation, archive, delivery, and closure fields separately. The final answer comes from `final.md`; absent final remains pending/unresolved rather than synthesized from votes.

### D3: Bounded daemon reads, typed client, read-only UI

Expose a paginated consultation list and one detail route under the existing knowledge API. Use the existing graph read pool and error envelope. A graph that is not configured returns the existing unavailable response. A specific missing record returns 404. DTOs contain stable IDs, requester display text, host, timestamps, statuses, phase sequence, per-consul artifacts, proposals, ballots, escalation evidence, final text, hashes and sync timestamp. All Markdown is rendered safely as untrusted content; links cannot navigate to local file paths. The page uses the generated API client, gives a clear empty state, and marks partial records and sync failures. No direct browser-to-Neo4j connection.

### D4: Schema and change rollout

Add a Neo4j uniqueness constraint for the consultation key and artifact key, plus indexes for chronology and host. Use parameterized Cypher through the existing graph client. Apply schema through the project's existing schema/operations path and make it safe to repeat. Start scanning only after graph wiring is ready; a bounded batch and retry interval prevent daemon startup from waiting for a large archive. The existing Neo4j backup process covers indexed copies while archive originals remain on each owner host.

## Risks / Trade-offs

- A host offline or without configured Neo4j cannot publish its archive until it reconnects. The UI must show last sync, and operations should expose scanner failure rather than silently imply completeness.
- Historic archives may lack requester or a clean ballot format. Display the raw record and `Unknown` for unproven derived fields.
- Consultation text may include sensitive project material. Use only existing authenticated remote-host routes, exclude logs and paths, cap payloads, and follow current graph access controls.
- Periodic scans add filesystem and Neo4j work. Bound work per pass and skip unchanged digests; expose measured scan duration and counts before tuning.

## Open Questions for geo.architect

1. Does archive-plus-Neo4j satisfy the intended single query storage while preserving the skill's canonical archive requirement?
2. Should ingestion run in each daemon or be an explicit skill close hook, given that the caller host may not keep its daemon running?
3. What exact metadata should be derived from freeform Markdown versus shown only as recorded text?
