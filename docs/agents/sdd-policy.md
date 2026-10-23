# SDD intake, review, and source of truth policy

## Context and decision

This is a documentation and contributor-process change. It changes no product runtime behavior, API, configuration, authentication, billing, release automation, or deployment behavior. Under D05, that classification does not exempt this change from a full proposal, geo.architect approval, independent PR review, or applicable checks.

The full proposal and its explicit D03 ruling are preserved in the issue's consultation archive. Each PR records the archive reference and the hash of the exact proposal revision it implements.

## Policy model

The owner decision source is [`docs/backlog/openspec/decisions.md`](https://github.com/petarnenov/ultra-factory/blob/989481f25805cecb16ef2ebed73b578e0fc01068/docs/backlog/openspec/decisions.md) at commit `989481f25805cecb16ef2ebed73b578e0fc01068` on `docs/openspec-sdd-blueprint`. This source is outside `dev`; the URL pins the exact planning revision.

### Scope and evidence

Apply D00–D13 to AO's supported repository surfaces: local daemon/CLI, desktop/web, mobile, Railway/self-hosted hub and BFF, and remote workers. Exclude AO Cloud public/private components and cloud-only contracts, tenant/billing/PostgreSQL behavior, and cloud deployment. Do not remove existing files or checks merely because they are excluded. Do not edit global agent skills or prompts.

Every change—runtime, docs, formatting, dependencies, tooling, CI, maintenance, automation, promotion-related work or declarations, or emergency—has one GitHub issue, one full OpenSpec change/proposal, and geo.architect review before implementation. A full proposal records why, scope and non-goals, impact and affected capabilities (or justified no-runtime behavior), stable requirements/scenarios, approach, tasks, verification, security/compatibility/operational risks, and rollback. A no-runtime-behavior declaration explains impact and uses documentation correctness/source/evidence scenarios; it never waives the proposal or review. Emergencies have no deferred-proposal exception.

### D03 ruling path

Use the repository's [geo.architect process](../../.agents/skills/geo.architect/SKILL.md) for independent analysis, mutual critique, immutable proposal identity, archived evidence, and normal council ratification when all consuls can decide. For an unresolved decision or unavailable consul, apply this exact project-specific order: Claude Code `claude-fable-5-1` medium; Codex `gpt-6-astra` medium; OpenCode `opencode/deepseek-v4.1-flash` provider default. The first available consul is the final arbiter, not an additional majority vote. A consul is unavailable only after an observed provider, harness, or access failure. A readiness probe is not a ruling. Obtain an explicit ruling on the immutable proposal revision. Preserve the reason for each earlier unavailability, available analysis/dissent, selected arbiter, exact proposal hash, explicit ruling, rationale, and terminal process closure in the consultation archive. Missing output, silence, timeout, or absent votes never imply approval. If all three cannot rule, the affected work stays blocked; independent approved work may continue. Do not substitute a model or reuse an approval from another issue.

### Roles, lifecycle, and readiness

- The capability owner identifies scope, affected capabilities, requirements, scenarios, evidence, risks, and follow-up ownership.
- The worker implements only the approved proposal and records completion evidence.
- An independent reviewer compares the impact declaration with the semantic diff, checks acceptance evidence, and records the evaluated head SHA.
- Only the project root may integrate an implementation PR into `dev`.

Readiness to merge requires an explicit proposal ruling for the exact proposal revision, independent approval for the exact final head, all applicable required CI successful on that same head, no unresolved blocking review, and complete acceptance evidence. A new commit or changed head invalidates review and CI readiness until rerun. Workers and coordinators do not merge. D10 ends at `dev`: it grants no authority to merge into `main`, promote, publish, release, or deploy. Completion requires verified `dev` integration and acceptance evidence, not a draft PR or a checked task box.

### Identity, declarations, and issue discipline

Each independently mergeable implementation issue owns exactly one OpenSpec change and one PR. GitHub issue #617 remains the umbrella execution tracker; do not keep a shared umbrella delta to reapply to child PRs. Stable requirement and scenario IDs remain stable when text changes; retire an ID explicitly rather than silently reassigning it.

Each PR names its single issue, full proposal/change ID, affected capabilities, stable requirement/scenario IDs, evidence, verification and rollback. When runtime behavior is unaffected, state `No runtime behavior change` and justify it. Docs-only, automation, and promotion-related changes still declare scope and impact explicitly; they receive no silent exemptions. Every issue has exactly one type label and exactly one priority label. Forms may collect intended type/priority values but do not apply labels; the existing issue-policy workflow remains authoritative for label enforcement.

Reviewers must assess whether declared impact matches the semantic diff and record the reviewed head SHA. Structural validation can check references and shape; it cannot discover undeclared behavior. Acceptance on `dev` means repository integration of an accepted specification, not that the behavior is in a released product. D11 reserves release promotion, publication, and deployment to the owner.

### Source of truth and contradictions

Authority is concern-specific. ADRs record architectural rationale and constraints. Generated contracts and their defined sources govern implemented API/storage contracts. Code and tests are evidence of current implementation, not automatic proof of intended behavior. An accepted OpenSpec requirement describes intended behavior on the current `dev` integration branch; it is not evidence of release. Human-facing prose explains intent and must be checked against authoritative artifacts. Live/deployed behavior needs evidence from that environment; source inspection alone is not deployment verification.

When code, tests, docs, ADRs, or generated contracts conflict, record the exact claims and source revisions in the proposal, then ask geo.architect to decide intended in-scope behavior using D03 arbitration. Do not choose code or prose automatically, weaken hard requirements, or hide a known defect. Keep unresolved implementation defects as explicit gaps with an owner and follow-up issue. If the decision changes an ADR or generated contract, use a separate approved implementation issue when outside current scope; synchronize affected docs/contracts and archive the OpenSpec change in the same final PR that delivers its approved change. Cite source evidence and exact revisions.

### D00–D13 translation

- **D00:** AO Cloud public/private components and cloud-only behavior remain excluded; existing files/checks are retained unless separately changed.
- **D01:** Railway/self-hosted hub, BFF, remote workers, security, operations, and recovery remain in scope; this policy does not start or deploy the hub.
- **D02:** Every full proposal follows the geo.architect process: independent analysis, mutual critique, immutable proposal identity, and normal council ballots/rules when consuls can decide. Changed proposal content invalidates prior approval.
- **D03:** Apply the explicit fallback order and archive/blocked rules above; this policy is initiative-specific and does not change the global skill.
- **D04:** macOS, Windows, Linux, iOS, Android, and Railway web remain in scope. Native behavior requires evidence from the actual platform; mocks/typechecks/other OS are not substitutes. Missing checks remain blocked or unverified, not passed.
- **D05:** Every change type, including documentation, formatting, dependencies, tooling, CI, maintenance, automation, promotion-related changes, and emergency fixes, receives full proposal/review before work. No-behavior classification never exempts it.
- **D06:** geo.architect resolves code/test/documentation contradictions from evidence, with D03 arbitration; preserve gaps and do not weaken hard requirements.
- **D07:** Agents may choose bounded early/broad pilot cases within approved scope. Each case still needs its own full proposal/ruling; geo.architect decides go/no-go from final evidence.
- **D08:** Measure and accumulate early performance/reliability observations with workload, platform, environment, revision, metric, sample count, and failures. Do not add new numeric blocking SLOs for early adoption; existing correctness/security/reliability checks remain required.
- **D09:** Continue independent approved work when hardware, credentials, or services are unavailable. Record the exact prerequisite, affected scenario/platform, owner, and follow-up verification issue; never fabricate a pass or claim full readiness.
- **D10:** Root-only autonomous merge into `dev` after exact-proposal approval, exact-final-head independent review, applicable successful CI, no blocking unresolved review, and acceptance evidence. Head changes invalidate readiness. No worker/coordinator merge.
- **D11:** Automation stops at `dev`; main promotion, release, publication, and deployment are owner-operated. Agents do not merge to `main`, start release/deployment workflows, or automatically create a promotion PR; approved work may prepare traceable evidence.
- **D12:** After the approved broad pilot and readiness evidence, root verifies intended check names and final-head behavior, positive/negative fixtures, workflow availability on the target branch, permissions, and absence of bypasses before activating required SDD checks. Preserve existing protections and record before/after settings. Returning to reporting-only or weakening enforcement requires explicit owner permission. Do not require a check on a branch where the workflow has not been promoted; track that prerequisite instead.
- **D13:** Follow the [OpenSpec workflow](openspec.md). When setting up or deliberately upgrading OpenSpec, resolve the official npm `latest` tag at setup time, record the timestamp, exact version, upstream source/release, Node requirement, and lockfile integrity, then use that exact version locally and in CI. CI never resolves mutable `latest`.

## Stable requirement and scenario IDs

Keep these identifiers stable as policy wording evolves. If a rule is retired,
record the retirement instead of reusing its ID for a different rule.

| Requirement | Scenario |
| --- | --- |
| R619-01: Translate D00–D13 locally, keep AO Cloud excluded, retain supported Railway/self-hosted scope, and do not change global skills. | S619-01: A task is checked against the pinned decision source and applies only within this repository's supported scope. |
| R619-02: Require a full proposal and ruling for every change type; a no-runtime classification never waives review. | S619-02: A documentation, formatting, tooling, CI, maintenance, automation, emergency, or promotion-related change starts with the same approval path and non-runtime evidence where appropriate. |
| R619-03: Apply the D03 order, observed-unavailability rule, explicit ruling, archive, and all-unavailable blocked state. | S619-03: Missing output or a successful model probe alone produces no approval; when all arbiters are unavailable, affected work remains blocked. |
| R619-04: Define roles, exact-final-head readiness, root-only `dev` integration, completion, and the D11 release boundary. | S619-04: A new commit invalidates readiness; only root integrates after proposal, independent review, applicable CI, and acceptance evidence pass on one head. |
| R619-05: Use one change per child issue/PR, stable IDs, required declarations, and exactly one issue type and priority. | S619-05: A PR identifies its proposal/change and affected requirements or justified no-runtime impact; #617 retains planning without a shared delta. |
| R619-06: Require semantic declaration-to-diff review and distinguish accepted `dev` intent from released behavior. | S619-06: Reviewer records the exact head and checks undeclared semantics; structural checks are not treated as proof of completeness. |
| R619-07: Resolve source contradictions through evidence and D06/D03; synchronize and archive approved work in the final PR. | S619-07: Conflicting code, test, prose, ADR, or generated contract claims are recorded and adjudicated without automatically preferring one artifact. |
| R619-08: Run #633 before #620 and exclude premature #626/#627/#631 work. | S619-08: Feature, bugfix, and maintenance walkthroughs are refined by the early pilot before broad baseline/gate/settings work. |

## Lifecycle walkthroughs

### Feature

A feature issue has its required type and priority labels, a capability owner and bounded outcome. Before implementation, create one full proposal with stable requirement/scenario IDs, affected capabilities, platform/security/operational scope, acceptance and rollback; obtain the explicit D03 ruling. The PR declares the approved change ID and impact, maps its semantic diff to scenarios, and records exact evidence and final-head review. Merge readiness is checked on the final head; root integrates to `dev`. This records accepted development behavior; release remains owner-operated.

### Bugfix

A bug issue records observed versus expected behavior and evidence, while preserving current code/test/docs disagreement rather than assuming one source is correct. The proposal identifies the contradiction, supported surfaces, known defect, scenarios, fix, and rollback. geo.architect decides intended behavior from evidence under D06/D03. The PR maps the fix to the approved scenarios and records platform tests. Unavailable device/service evidence remains explicitly unverified with an owned follow-up; it is not a pass.

### Maintenance

A dependency, formatting, documentation, or automation maintenance issue still receives a full proposal and ruling before work. If runtime behavior does not change, the proposal declares why and uses source/link/format/fixture checks as meaningful non-runtime acceptance; it does not invent a product requirement. Reviewers compare the actual diff with the declaration and record the final head. No special docs-only or automated-change exemption applies.

## SDD-15 and future gates

Issue #633 runs the bounded early pilot after #619 and refines these conventions before #620 broad capability baselining. This change does not implement #626 evidence validators, #627 CI gates, #631 required settings, #634 archive enforcement, or any application runtime integration. Root owns later readiness and activation decisions under D12.

## Risk and rollback

The principal risk is confusing accepted development intent, current implementation, and released behavior. Keep the source hierarchy and environment evidence explicit. Another risk is treating structural validation as semantic review; require a human reviewer to compare declaration and diff on the recorded head. If this policy is materially wrong, revert the focused PR on `dev` and preserve the ruling/archive and issue evidence. A corrective policy is a new fully proposed and reviewed change.

The same-PR archive rule applies prospectively to the approved change being delivered. It does not require this PR to archive `sdd-01-openspec-bootstrap` or treat that separate #618 change as defective.
