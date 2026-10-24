# Tasks

## 1. Proposal and architecture

- [x] 1.1 Validate the complete OpenSpec proposal and obtain `geo.architect` decision on D1-D4 and the open questions through the authorized D03 Fable 5.1 fallback arbitration. No council votes were obtained.
- [x] 1.2 Apply arbitration amendments to proposal, design and requirements before implementation.

## 2. Backend and storage

- [x] 2.1 Add repeatable Neo4j schema and bounded, parameterized consultation write/read operations.
- [x] 2.2 Implement safe local archive discovery, explicit state-field allowlist, strict ballot grammar, full-file hashing, content cap, idempotent ingestion, retry, observable scan state and `AO_KNOWLEDGE_GRAPH_CONSULTATION_SCAN=0` opt-out.
- [x] 2.3 Add daemon list/detail HTTP routes and DTOs, regenerate OpenAPI and frontend client types.
- [ ] 2.4 Test complete, partial, malformed and unavailable archives; duplicate import; host collision; truncation; 501/503/404 API responses, pagination and error envelopes.

## 3. Supervisor page

- [x] 3.1 Add navigation and consultation list/detail UI showing requester, consul artifacts, critiques, escalations, rounds and final answer.
- [x] 3.2 Test complete, partial, empty and unavailable views and safe Markdown rendering with no file opener.

## 4. Verification and delivery

- [x] 4.1 Document schema operation, graph configuration on every consultation host, host catch-up, backfill and recovery; update the skill archive contract to clarify the graph index is not a replacement archive.
- [ ] 4.2 Run focused and full required local CI gates serially, measure touched Go package coverage, and arrange independent review.
- [ ] 4.3 Commit, push, open a PR to `dev` for issue #639 with verified change counts, and verify remote checks.
