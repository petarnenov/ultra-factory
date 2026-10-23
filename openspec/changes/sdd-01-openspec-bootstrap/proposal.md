# Proposal

## Why

The repository has no initialized OpenSpec workflow. Issue #618 establishes a reproducible, repository-local toolchain for the next SDD tasks while keeping tool behavior fixed for every revision.

## What Changes

- Pin the setup-time official `@fission-ai/openspec` latest release as an exact root development dependency with lockfile integrity.
- Initialize the upstream `spec-driven` schema and one sample change for this bootstrap.
- Generate and inspect repository-local Claude Code, Codex, and OpenCode 2 guidance without replacing existing project rules.
- Document tested CLI equivalents, supported profiles, provenance, and deliberate upgrades.
- Validate the real sample and a malformed fixture locally and in the Node 24 CI install job.

## Capabilities

### New Capabilities

None. This change establishes authoring and validation tooling; it does not introduce a product capability.

### Modified Capabilities

None. `.openspec.yaml` marks this change `skip_specs: true` because it has no runtime behavior delta.

## Impact

Changes are limited to the root development manifest and lockfile, `.gitignore` allowlisting the generated OpenSpec skills, `openspec/`, contributor guidance, a fixture/check script, and the existing Node 24 `api-drift` job. There are no daemon, CLI product, API, database, desktop, web, mobile, Railway, cloud, release, or deployment changes.

This proposal is for #618 only and owns exactly one OpenSpec change: `sdd-01-openspec-bootstrap`.

## Approval boundary

Until #619 lands D03 in repository-local policy, retain the owner’s exact fallback rule here:

> If the council does not decide or a consul is unavailable, the first AVAILABLE of these gives the final explicit ruling on the exact proposal revision: (1) Claude Code claude-fable-5-1 medium, (2) Codex gpt-6-astra medium, (3) OpenCode opencode/deepseek-v4.1-flash provider default. Unavailability requires an observed provider/harness/access failure; never infer approval from silence, missing ballots or timeouts. Preserve dissent and fallback evidence.

The exact external proposal, ruling, analyses, dissent, and fallback evidence are archived at `/Users/petarnenov/.ao/architecture-consults/archive/2026-10-23/openspec-618-bootstrap/`. The approved proposal-1 SHA-256 is `b5357dbcaea197f0bbdb2c024715909ccedce9d42855c8e64fa502ec5926eeab`. The recorded D03 ruling approves implementation of that exact revision; it does not authorize merge, issue closure, promotion, publication, or deployment.
