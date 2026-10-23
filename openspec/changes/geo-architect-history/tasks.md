# Tasks

## 1. Proposal and architecture

- [ ] 1.1 Validate the complete OpenSpec proposal and obtain `geo.architect` decision on D1-D4 and the open questions.
- [ ] 1.2 Apply council amendments to proposal, design and requirements before implementation.

## 2. Backend and storage

- [ ] 2.1 Add repeatable Neo4j schema and bounded, parameterized consultation write/read operations.
- [ ] 2.2 Implement safe local archive discovery, parsing, hashing, idempotent ingestion, retry and observable scan state.
- [ ] 2.3 Add daemon list/detail HTTP routes and DTOs, regenerate OpenAPI and frontend client types.
- [ ] 2.4 Test complete, partial, malformed and unavailable archives; duplicate import; API pagination and error envelopes.

## 3. Supervisor page

- [ ] 3.1 Add navigation and consultation list/detail UI showing requester, consul artifacts, critiques, escalations, rounds and final answer.
- [ ] 3.2 Test complete, partial, empty and unavailable views and safe Markdown rendering.

## 4. Verification and delivery

- [ ] 4.1 Document schema operation, host catch-up, backfill and recovery.
- [ ] 4.2 Run focused and full required local CI gates serially, measure touched Go package coverage, and arrange independent review.
- [ ] 4.3 Commit, push, open a PR to `dev` for issue #639 with verified change counts, and verify remote checks.
