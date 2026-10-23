# Proposal 619-1: Repository-local SDD intake, review, and source-of-truth policy

## Why

Issue #619 requires the confirmed OpenSpec adoption decisions D00–D13 to become usable repository policy after #618, without changing unrelated projects or assuming that documentation already describes the merged `dev` state. The current `dev` head is `f891dd1db92b1e52a0c5997ed56a26d9e8ba1b2a`; it contains #618. Existing guidance pins OpenSpec and identifies source artifacts, but does not yet define initiative-wide intake, proposal approval, semantic review, readiness, or source conflict handling.

## Scope

One independently mergeable OpenSpec change for #619 and one PR to `dev`, limited to `AGENTS.md` pointers, `docs/agents/`, `docs/documentation-map.md`, GitHub issue forms, the PR template, and this change's OpenSpec artifacts. Add stable requirement/scenario IDs and a repository-local policy translating D00–D13. Keep #617 as the GitHub execution tracker; each child issue owns exactly one change and PR.

## Non-goals

No validators or CI gates from #626/#627, archive enforcement from #634, settings or product integration from #631, capability baseline from #620, broad pilot from #633, shared/umbrella delta, global skill edits, runtime behavior, auth/billing/config changes, KG/shared-data edits, issue closure, merge, release, deployment, or production operation. Do not reuse #639's ruling.

## Impact and no-runtime-behavior rationale

Documentation, forms, and templates only define contributor/agent process. They do not alter application runtime behavior. This is still a full D05 change and requires this proposal, geo.architect approval, PR semantic review, and applicable repository checks. Non-runtime acceptance is correct policy translation, traceable links/IDs, rendered/valid forms and template, and examples whose stated authorities match the source records and current code/contracts.

## Stable requirements and scenarios

- R619-01 / S619-01: D00–D13 are translated repository-locally; AO Cloud remains excluded while supported Railway/self-hosted and remote-worker surfaces remain in scope; no global skill changes.
- R619-02 / S619-02: Every change type (runtime, docs, formatting, generated/automated, maintenance, emergency, promotion/declaration) needs a full proposal and geo.architect review before execution; no-behavior classification never waives review. Text-only scenarios test documentation claims without inventing runtime behavior.
- R619-03 / S619-03: D03 arbitration is Fable 5.1 medium, then Codex Astra medium, then OpenCode DeepSeek Flash 4.1 default; the first available consul gives an explicit ruling on the immutable proposal/revision. Explain each higher-priority unavailability. Silence/timeouts are not decisions; if all unavailable, block affected work. Preserve analyses, dissent, ruling, hashes and terminal process closure in the consultation archive. No substitution or reuse of #639.
- R619-04 / S619-04: Roles are capability owner (scope/evidence), worker (approved implementation), reviewer (independent semantic diff check on exact head), and root (readiness and autonomous `dev` integration only); coordinators/workers cannot merge. Readiness requires approved exact proposal, independent approval on final head, all applicable required CI on that head, no blocking unresolved review, and acceptance evidence. Any head change invalidates readiness. Completion requires verified `dev` integration and evidence. No main, release, publication or deployment authority is implied.
- R619-05 / S619-05: One OpenSpec change per independently mergeable child issue/PR; stable requirement/scenario IDs survive revisions; umbrella planning remains in #617 and no umbrella delta is copied/reapplied. Each PR names issue, change ID/full proposal, affected capabilities or justified no-runtime classification, scenarios/evidence, verification and rollback. Exactly one issue per PR and exactly one type and priority label.
- R619-06 / S619-06: Reviewers compare declarations to semantic diff and record evaluated head SHA; machine checks can validate structure/references but cannot discover undeclared semantics. Docs-only, automation and promotion declarations are explicit, with no silent exemptions. Accepted on `dev` means repository integration, not released product behavior.
- R619-07 / S619-07: D06 contradictions among code, tests, docs, ADRs and generated contracts go to geo.architect with source evidence and D03 arbitration; record intended behavior without weakening hard constraints. ADRs explain decisions; generated contracts and their authoritative source define implemented behavior. Update/synchronize affected authoritative docs/contracts and archive the completed change in the same final PR before declaring completion; link evidence and retain defects as gaps.
- R619-08 / S619-08: Feature, bugfix and maintenance walkthroughs demonstrate the lifecycle. #633 early pilot refines these conventions before #620 broad baselining. Do not implement #626 validators, #627 gates, or #631 settings here.

## Approach

Update the root agent guidance with a pointer; add a focused SDD policy page under `docs/agents/`; extend the documentation map's source-of-truth and synchronization rules; adjust issue forms to capture issue type/priority, capability, proposal/change ID and evidence without falsely treating intake as approval; adjust the PR template for proposal identity, impact declaration, semantic review/head, applicable CI and dev-vs-release status. Create one OpenSpec change with policy delta/spec, rationale, tasks, stable IDs, and examples. Keep all wording aligned to existing issue policy, #618 pinned CLI usage, actual code/contracts, and D00–D13.

## Tasks

1. Draft and receive explicit geo.architect ruling on the immutable full proposal and SHA-256 before implementation.
2. Create one `sdd-02-intake-review-source-of-truth` change via root-pinned OpenSpec 1.14.3 commands.
3. Write policy and integration-point edits; add feature, bugfix and maintenance walkthroughs and conflict/source-evidence procedure.
4. Validate OpenSpec artifacts, links, forms/template and examples; run applicable lightweight repository documentation/policy checks and all applicable CI jobs serially with host coordination. Record unavailable environment checks exactly.
5. Reconcile exact changed files and requirement/scenario evidence; commit, push, create draft PR to `dev` with `Closes #619`, required count header, full-proposal/hash and impact declaration.
6. Obtain native review on pushed exact head; address every finding, rerun applicable checks, verify review and CI against final head, and hand off root-ready evidence without merging or closing the issue.

## Verification

Check all local links and referenced source paths; validate OpenSpec with pinned `npm run openspec -- validate <id> --strict --no-interactive`; check issue form YAML and PR template; compare claims against D00–D13, source contracts/ADRs and current `dev`; walk the feature, bugfix and maintenance examples; run the applicable existing policy/documentation CI and complete local matrix that can run. Do not call remote-only/native/credential-dependent checks local passes. Record exact command, result, environment and final SHA. Reviewer's semantic completeness check records the exact reviewed head. Root verifies proposal approval, review, applicable CI and no blocking comments on the same final head; push invalidates readiness.

## Risks and compatibility

Policy may be mistaken for runtime support or machine enforcement; state explicitly that structural checks cannot establish semantic completeness and docs describe accepted `dev` state versus released behavior. Existing issue-form type defaults must align with required issue labels and not remove priority choice. SDD #633 may reveal convention changes, so scope examples as early-pilot-ready while making #633 the required refinement before #620. Keep source hierarchy explicit to avoid treating prose, tests, generated files, or code as universally dominant outside their defined authority.

## Rollback

Revert the single PR on `dev` if the policy is materially wrong; preserve consultation/archive and issue evidence. Do not weaken existing CI, issue policy, release boundary, or security constraints as part of rollback. Any corrective policy remains a new full-proposal change.
