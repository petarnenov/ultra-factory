# Design

## Context

See `proposal.md` for the six approved capability paths and non-goals. This change records current observable contracts without changing Go behavior. `openspec/specs/openspec-check-setup/spec.md` is the only existing capability, so all six approved paths are new.

## Goals / Non-Goals

**Goals:** Keep each core capability cohesive; carry the approved stable requirement/scenario identifiers into the accepted specs; map each scenario to concrete sources and tests; and preserve the D06 workspace ruling.

**Non-Goals:** Fix runtime defects, change DTOs or routes, add migrations or queries, duplicate #28/#29 capabilities, or certify unrun tests or deployed behavior.

## Decisions

1. **Use six separate capabilities.** Session status, workspace lifecycle, Chat/TUI recovery, daemon errors, CLI errors, and SQLite upgrades/CDC have distinct observable contracts and owners. A catch-all core spec would obscure those boundaries. Stable identifiers and complete scenario mappings live in the delta files and `verification.md`.
2. **Treat code-first contract sources as authority.** Daemon DTOs and `apispec/specgen/build.go` define the wire contract; generated OpenAPI and frontend types are outputs. SQLite migrations and queries are handwritten schema/query inputs, `db.go` also contains compatibility schema repair DDL for known histories, and `gen/` is generated sqlc output.
3. **Keep uncertain observations uncertain.** A failed or conflicting runtime probe alone does not imply death. Display status remains derived from durable facts.
4. **Apply the D06 workspace ruling narrowly.** Ordinary cleanup refuses and preserves dirty registered worktrees. Direct uncaptured `ForceDestroy` is a defect tracked by #659; the specs make no force-path safety claim, and no preserve-before-force exception is approved.
5. **Use test names as evidence links, not as pass claims.** `verification.md` identifies exact functions and tag requirements. Validation records which selected tests actually ran; no unselected Go CI job is reported as passed.

## Risks / Trade-offs

- [Existing docs or tests may not represent all production behavior] → Keep scenario language bounded to named evidence and record missing behavior as a gap.
- [Compatibility schema repair could be mistaken for a migration rewrite] → Cite `db.go` repair DDL explicitly while preserving append-only shipped migrations.
- [ForceDestroy callers may rely on an unsafe path] → Keep the prohibition normative and track direct refusal, capture failure, races, recovery, and uncaptured file limits in #659.
- [A listed test may be skipped in a default command] → Mark `TestE2E_ExitCodes` as `e2e`-tagged and report its actual execution separately.

## Migration Plan

There is no runtime data migration. Validate and review the proposal, six deltas, source/test map, and exact D06 scope; archive the OpenSpec change in this PR after its delivery requirements pass. Rollback is a reviewed follow-up removing only this #620 documentation baseline; no database state or runtime behavior is affected.
