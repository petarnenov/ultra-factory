# Design

## Context

The consultation skill writes an immutable, structured archive under `~/.ao/architecture-consults/archive/<UTC date>/<id>/`. It has `request.md`, `state.json`, independent analyses, critiques, versioned proposals, ballots, `final.md`, and per-consul collected reports. Some consultations may be incomplete. AO already has an optional central Neo4j knowledge graph through its daemon; a graph is suitable for consultation, round, consul, artifact, and decision relationships. A frontend cannot directly read another host's files, and it must not receive arbitrary file paths.

## Goals / Non-Goals

**Goals:** Display the exact recorded content and provenance; keep the consultation navigable across hosts; survive restart and graph downtime; avoid duplicate records on retry; preserve honest process and decision status; keep daemon and UI boundaries; follow the OpenSpec change lifecycle.

**Non-Goals:** Run new consultations from this page, edit or ratify decisions, import CLI logs or credentials, replace the skill's local archive during this change, introduce a new network listener, or alter the three-consul voting rules.

## Decisions

### D1: Local archive is evidence; Neo4j is the query index and retained copy

The caller host keeps the skill-required archive. Each AO host with Neo4j configured scans only its own canonical archive under `<parent of resolved daemon dataDir>/architecture-consults/archive` (normally `~/.ao/architecture-consults/archive`) at daemon startup and periodically thereafter. The index key is `<daemonHostId>/<UTC date>/<consultationId>`; the host recorded in `state.json` is separate. It validates the date/ID layout, an exact artifact allowlist, file type, and symlinks. It hashes full file bytes, caps graph-stored content at 256 KiB per artifact with an explicit `truncated` flag, and upserts the consultation and all observed artifacts in one parameterized transaction. A change in bytes updates that artifact and scan metadata; vanished files keep their last content with `absentSince`. Ingestion failures leave files intact and are retried. The scanner never writes to the archive, follows no symlinks outside the archive tree, and imports no logs. Absolute local paths, process/session handles, caller channels and message IDs stay out of Neo4j and API responses. This Neo4j index is not the store-backed replacement for the archive.

Alternatives: Graph-only write at skill execution would make failure of Neo4j block archiving and require changing the skill; a new SQLite store would fragment cross-host browsing and duplicate AO's existing graph infrastructure. The retained archive plus Neo4j index preserves the evidence and supports central reads.

### D2: Model rounds explicitly

Map `independent/` to independent analysis, `critique/` to mutual critique, and each immutable `proposal-N.md` to a proposal version, and observed `ballots/round-N/` directories to ratification rounds. Expose the exact observed number of ratification rounds and a separate count of phase rounds, so the page never implies a ballot round existed when only analysis or critique was completed. Show per-consul original artifact content, expected-but-missing artifacts, and ballot stance only when explicit decision IDs and stance match a documented unambiguous grammar. Only allowlisted structured `state.json` fields may drive status; freeform prose remains evidence. Preserve escalation, archive, delivery, closure, sync and decision states separately. The final answer comes from `final.md`; absent final remains pending/unresolved rather than synthesized from votes. A D03 fallback arbitration is displayed as arbitration, never as three-consul voting. The explicitly allowed original `fallback-arbitration/fable.md` report appears as arbitration evidence; its proposal alone does not create a voting round. Requester comes only from a structured string field in `state.json`, never from owner or request prose.

### D3: Bounded daemon reads, typed client, read-only UI

Expose `GET /api/v1/knowledge/consultations` with limit 1–50 and an opaque cursor, and `GET /api/v1/knowledge/consultations/{id}` with a URL-safe opaque ID formed by base64url encoding the composite graph key. Use the existing graph read pool, a bounded read timeout and the API error envelope. Return 501 when the graph is not configured, 503 when unreachable and 404 for a missing key. DTOs contain stable IDs, requester display text, recorded host, timestamps, distinct statuses, phase sequence, per-consul artifacts, proposals, ballots, escalation evidence, final text, hashes, truncation flags and sync timestamp. They contain no file paths or logs. Render Markdown with the existing safe-origin renderer and without a file-opening link provider. The page uses the generated API client and marks partial records and scan failures. No direct browser-to-Neo4j connection.

### D4: Schema and change rollout

Add a Neo4j uniqueness constraint for the consultation key and artifact key, plus indexes for chronology and host. Use parameterized Cypher through the existing graph client. Apply schema through the project's existing schema/operations path and make it safe to repeat. Start scanning only after graph wiring is ready; a bounded batch and retry interval prevent daemon startup from waiting for a large archive. `AO_KNOWLEDGE_GRAPH_CONSULTATION_SCAN=0` disables scanning, and each participating host requires graph configuration. Expose last scan time, counts and error without suggesting global completeness. The existing Neo4j backup process covers indexed copies while archive originals remain on each owner host.

## Risks / Trade-offs

- A host offline or without configured Neo4j cannot publish its archive until it reconnects. The UI must show last sync, and operations should expose scanner failure rather than silently imply completeness.
- Historic archives may lack requester or a clean ballot format. Display the raw record and `Unknown` for unproven derived fields.
- Consultation text may include sensitive project material. Use only existing authenticated remote-host routes, exclude logs and paths, cap payloads, and follow current graph access controls.
- Periodic scans add filesystem and Neo4j work. Bound work per pass and skip unchanged digests; expose measured scan duration and counts before tuning.
- A missing or ambiguous ballot grammar cannot be converted into a stance. Show the original ballot text and mark the stance unknown.

## Open Questions for geo.architect

Resolved by D03 fallback arbitration on 2026-10-23. The three-consul council did not run because OpenCode's read-only plan-agent boundary could not be verified. Claude Fable 5.1 at medium effort supplied the single authorized fallback ruling on pinned commit `d418fb8bb777df1d19dd6a2f03908a2fbcf5788e`; there are no council votes or consensus claim. The full ruling is archived on the iMac under `~/.ao/architecture-consults/archive/2026-10-23/ultra-factory-issue-639-d418fb8-20261023T211546Z/`.

1. Archive plus Neo4j is approved: files remain canonical, while Neo4j is the only query index.
2. Each participating daemon performs bounded startup and periodic scans. No skill close hook is required.
3. Only explicit allowlisted state fields and unambiguous ballot grammar may become structured facts. Freeform prose remains original evidence.
