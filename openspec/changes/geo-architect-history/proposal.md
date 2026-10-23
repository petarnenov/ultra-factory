# Proposal

## Why

`geo.architect` already retains detailed consultation archives, but AO has no place to inspect who requested a consultation, what each consul proposed, how their positions changed, what objections were escalated, how many rounds ran, or what decision was delivered. The information is scattered across files on the caller host and cannot be browsed from the supervisor. This change makes the recorded evidence accessible without converting missing artifacts into apparent agreement.

## What Changes

- Add a read-only AO page with a consultation list and a detailed timeline of request, independent analyses, mutual critiques, proposal versions, per-round ballots, escalations, and final answer.
- Ingest verified `geo.architect` archive artifacts into the existing Neo4j knowledge graph with stable host/date/consultation IDs. Retain the original files as canonical, read-only evidence and make re-ingestion idempotent. Neo4j is the sole query index, not an archive replacement.
- Expose bounded list and detail reads through authenticated remote-host and existing loopback daemon API routes, including explicit incomplete, unavailable, and unsynced states.
- Capture requester and archive host from the recorded archive metadata; show unavailable when the archive did not record a field. Preserve original consul text and distinguish factual objections from process/status changes.
- Provide initial backfill and ongoing bounded catch-up for archives on participating AO hosts, with a scanner opt-out. Each consultation host needs graph configuration; failed ingestion remains retryable and observable.
- Add tests for archive parsing, idempotent graph writes, API behavior, and the UI's complete and incomplete views. Document operation and recovery.

## Capabilities

### New Capabilities

- `architecture-consultation-history`: Durable indexing and faithful browsing of `geo.architect` consultation evidence and outcomes.

### Modified Capabilities

None.

## Impact

- Backend knowledge graph adapter, a bounded archive importer, daemon lifecycle, HTTP DTOs and routes, generated OpenAPI/client types.
- Frontend supervisor navigation and consultation list/detail page.
- Neo4j schema, scanner status and runbook. No new storage service or database dependency.
- `geo.architect` archive contract remains the source of original reports. No AO Cloud integration, production deployment, or `dev` to `main` promotion.
