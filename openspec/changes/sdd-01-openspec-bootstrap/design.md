# Design

## Context

See [proposal.md](proposal.md). The root `api-drift` job already installs `package-lock.json` using Node 24, providing an existing CI location to verify the pinned tool and sample.

## Goals / Non-Goals

**Goals:** Reproducible local and CI validation, visible generated profile files, documented tested CLI usage, and a negative fixture that proves validation rejects malformed input.

**Non-Goals:** Runtime integration, release packaging, broad SDD traceability enforcement, new required checks, or changes to AO Cloud/Railway behavior.

## Decisions

- **Pin in the root development dependencies.** The root lockfile is already installed by `api-drift` on Node 24. A separate tools package would add another lockfile and CI installation path. An ephemeral `npx` invocation would not provide this repository’s lockfile integrity guarantee.
- **Use the upstream `spec-driven` schema.** A custom schema is unnecessary for this bootstrap and would risk encoding later SDD policy prematurely.
- **Integrate validation into `api-drift`.** That job already performs clean root `npm ci` on Node 24. Add one validation script after install instead of creating a broad or required SDD gate.
- **Keep one OpenSpec change and mark it no-spec-delta.** This is tooling and contributor workflow with no runtime behavior; `.openspec.yaml` uses `skip_specs: true` rather than inventing a product capability.
- **Track generated Codex skills narrowly.** `.gitignore` continues ignoring local agent skills generally, with exceptions only for upstream `openspec-*` files generated for this repository.

## Risks / Trade-offs

- **Generated files may conflict with repository guidance** → Inspect all generated profiles and state that project `AGENTS.md` remains authoritative; keep it and `CLAUDE.md` unchanged.
- **The malformed check may fail for an incidental reason** → Assert the expected missing-scenario diagnostic and validate a repaired control.
- **OpenSpec adds a development supply-chain dependency** → Pin exact version and integrity; install without adding secrets or product runtime dependencies.
- **Upstream command behavior can change** → Document only commands exercised with the pinned executable and require a reviewed exact-version upgrade.

## Migration Plan

No data migration or deployment applies. To roll back before integration, revert this single PR’s dependency/lockfile, generated OpenSpec files, docs, fixture/check, ignore exceptions, and CI step. Preserve consultation evidence. A later rollback is a separately reviewed change.
