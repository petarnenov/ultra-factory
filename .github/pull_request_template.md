Code changes: +<added> -<deleted>\
Tests: +<added> -<deleted>\
Others: +<added> -<deleted>

## What

Brief description of the change.

## Why

Problem this solves / issue it closes (e.g. `Fixes #123`).

## How

Key implementation decisions, trade-offs, or areas reviewers should focus on.
Call out intentional omissions (especially vs older TypeScript behavior) when relevant.

## SDD declaration

- Issue (one issue per PR; use `Closes #NNN`):
- OpenSpec change ID:
- Full approved proposal revision and SHA-256 / consultation archive:
- Affected capabilities and requirement/scenario IDs, or justified `No runtime behavior change` classification:
- Impact on supported platforms, security, operations, APIs, and release behavior:
- Acceptance evidence and rollback:

Every change type, including documentation, formatting, dependencies, tooling,
CI, maintenance, automation, promotion-related work, and emergencies,
requires a full proposal and explicit geo.architect ruling before implementation.
A no-runtime-behavior declaration does not waive these requirements. See the
[repository SDD policy](../docs/agents/sdd-policy.md).

## Testing

How you validated this locally (commands, scenarios, screenshots if UI).

- Applicable checks and exact results on the final head:
- Unavailable checks and owned follow-up (do not report these as passed):
- Runtime evidence, or non-runtime documentation/source/link evidence:

## Final-head review and status

- Semantic declaration-to-diff review completed by:
- Reviewed head SHA:
- Applicable required CI final-head status:
- Blocking review threads remaining:
- Accepted on `dev` means development integration only; released behavior and
  promotion to `main`, release, publication, and deployment remain owner-operated.

## Checklist

- [ ] Branched from `dev` and targets `dev` (or continuing an existing PR branch)
- [ ] One focused change; references its issue with `Closes #123` (see [docs/issue-policy.md](https://github.com/Untrivial-ai/agent-orchestrator/blob/main/docs/issue-policy.md))
- [ ] Follows [AGENTS.md](https://github.com/Untrivial-ai/agent-orchestrator/blob/main/AGENTS.md) conventions and PR hygiene
- [ ] Tests added/updated for user-visible behavior where it makes sense
- [ ] Relevant CI checks pass for the area touched (`go`, frontend, etc.)
