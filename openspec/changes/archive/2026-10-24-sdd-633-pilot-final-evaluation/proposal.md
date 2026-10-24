# Proposal

## Why

The bounded #647 pilot is integrated, but its evidence exposed convention gaps in archive command selection, manual artifact-completeness review, and tasks completed after archival. Issue #633 must record the actual pilot evidence and resolve these gaps before broad #620 baselining can begin.

## What Changes

- Record the #633 pilot outcome, evidence, measured friction, decisions, and owned follow-ups in the accompanying evaluation report.
- Clarify both pinned archive command forms in `docs/agents/openspec.md`, selected by whether a behavior spec delta exists.
- Require reviewers to record an exact-head manual completeness disposition and define how post-archive delivery tasks are reported until #634 / SDD-16 provides enforcement.
- Create and archive this documentation-only OpenSpec change in the same PR. It has no behavior requirement or spec delta, so `.openspec.yaml` sets `skip_specs: true`; archive with `--skip-specs` only on that basis.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

None. This change adds no behavior spec delta.

## Impact

Affected files are `docs/agents/openspec.md`, this change and its archive under `openspec/changes/`, and the #633 evaluation report. No runtime behavior, API, configuration, CI, workflow, Nix, README toolchain, package, release, or deployment changes. The PR targets `dev` and closes only #633.

## Scope and Non-goals

This records one bounded pilot and refines the repository-local OpenSpec workflow page before #620. It does not change #647's archive, #657 workflow ownership, #634 enforcement, CI path filters, the #627 gate, global agent instructions, or application behavior. It does not start #620 and does not promote, release, or deploy.

## Requirements and Scenarios

Keep the established #633 identities stable:

- **R633-01 / S633-01:** A bounded child is selected without a separate owner gate and receives its own proposal and ruling.
- **R633-02 / S633-02:** One child issue/change/PR delivers one accepted capability with mapped tests, manual evidence, semantic review, exact reviewed head, spec synchronization, archive, selected checks, and verified merge.
- **R633-03 / S633-03:** Pilot friction is recorded; incomplete artifacts are manually rejected until enforcement exists; the report distinguishes review, archive, post-archive tasks, and owned follow-ups.
- **R633-04 / S633-04:** Preserve the fresh evidence-based final conditional GO/NO-GO ruling and archive identity; this proposal does not replace it.
- **R633-05 / S633-05:** Stop at accepted `dev` behavior; do not claim release or deployment.

## Verification

Use Node 24.21.0, npm 11.19.0, and OpenSpec 1.14.3 through the root npm wrapper. Strictly validate this change, archive it in the same PR with `--skip-specs` only because it has no behavior spec delta, verify `openspec/specs/` is unchanged, preserve raw command outputs, and manually review the complete change at the final PR head. Run applicable policy checks and report unselected workflows as gaps. The independent reviewer records the final-head manual completeness disposition in the PR.

## Risks and Rollback

A wrong archive command could omit a behavior spec delta; choosing by delta presence, recording the `--skip-specs` reason, and verifying unchanged specs mitigate that risk. Manual review must not be described as automated enforcement. A post-archive delivery task must have a justified exception and completion evidence in the PR. Root may revert the focused PR on `dev` if these conventions prove incorrect, preserving consultation and historical child evidence.
