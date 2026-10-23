# Tasks

## 1. Pin and initialize

- [x] 1.1 Resolve official npm `latest` at approved setup and record version, upstream release identity, Node requirement, registry integrity, and local Node/npm versions in `docs/agents/openspec.md`.
- [x] 1.2 Add the exact root development dependency and lockfile; initialize `spec-driven` and the Claude Code, Codex, and OpenCode profiles; verify all generated skills and commands are tracked and existing `AGENTS.md`/`CLAUDE.md` remain unchanged.

## 2. Sample, verification, and contributor guidance

- [x] 2.1 Create this single #618 sample change with a no-runtime-behavior classification and validate it with the pinned CLI.
- [x] 2.2 Add a malformed fixture and validation script that checks the intended rejection and a repaired passing control; verify `npm run openspec:check` succeeds.
- [x] 2.3 Document the tested create, status, instructions, validate, and archive commands, supported profiles, no-runtime/release boundary, and explicit upgrade process; check each command against OpenSpec 1.14.3.

## 3. CI and delivery verification

- [x] 3.1 Run the bootstrap check after root `npm ci` in the Node 24 `api-drift` job; confirm the job uses the committed lockfile and never resolves `latest`.
- [ ] 3.2 Run complete applicable CI and verify final-head AO review and remote checks; report skipped draft jobs as unrun and hand off to root without merging or closing #618.
