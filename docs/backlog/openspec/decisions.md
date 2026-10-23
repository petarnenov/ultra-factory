# Owner decisions for machine implementation

Status: owner interview in progress. Do not schedule implementation from this backlog until the pending decisions below are resolved and reconciled into the blueprint and issue acceptance criteria. This record authorizes planning changes only; it does not start implementation, merge, release or deployment.

## Confirmed decisions

### D00 Exclude AO Cloud

Owner instruction: AO Cloud will not be used in this fork. Exclude both its public control plane and private submodule, tenant/billing/PostgreSQL behavior, cloud-only contracts/client behavior, deployment and private-owner evidence from this SDD adoption. Existing files are not deleted and existing CI checks are not removed. Shared code used by supported local or Railway surfaces remains in scope for those consumers only.

### D01 Retain Railway and self hosted remote workers

Owner answer: Yes to retaining Railway/self-hosted, hub/BFF and remote workers while excluding AO Cloud. Preserve their security, operational and recovery scenarios. This does not authorize starting the stopped Railway hub or deploying anything.

### D02 Proposal approval by geo.architect

Owner answer: Ask geo.architect. Behavioral proposals use the repository's `.agents/skills/geo.architect/SKILL.md` before implementation. Use its independent analysis, mutual critique, proposal identity and archived evidence. Normal council ratification uses its voting rules; D03 overrides its requirement for all three successful ballots and unresolved-decision escalation when the council cannot decide or a consul is unavailable. A completed consultation does not grant merge, release, spending or deployment authority. Documentation-only/no-behavior handling is pending D05.

Changed constraints or proposal content invalidate affected approvals and require review of the new version. This selects the future approval mechanism; no council has been launched or approval claimed during this interview.

### D03 Final decision authority and fallback order

Owner instruction: Fable 5.1 decides conclusively; if unavailable, Codex Astra decides; if also unavailable, OpenCode DeepSeek Flash 4.1 decides. For an unresolved council decision or an unavailable consul, obtain an explicit final ruling on the exact proposal/revision from the first available consul in this order:

1. Claude Code, `claude-fable-5-1`, medium effort.
2. Codex, `gpt-6-astra`, medium effort.
3. OpenCode, `opencode/deepseek-v4.1-flash`, provider default effort.

The selected consul is the final arbiter, not an extra majority vote. Preserve available analyses, dissent, the reason a higher-priority consul is unavailable, the selected arbiter and its explicit decision with rationale in the consultation archive. An observed provider/harness/access failure can establish unavailability; silence or an observation timeout alone is not a decision or proof that a live consultation ended. Do not substitute another model.

This owner-approved project policy overrides the skill's all-three-ballots requirement and unresolved-council human escalation for this initiative. The arbiter must resolve documented objections within approved scope and existing hard requirements; a decision does not itself grant missing credentials or authority to deploy, spend, merge or expand product scope. If none of the three can provide a ruling, no approval exists: keep the affected task blocked and report the missing prerequisite. Independent already-approved work can continue. No automatic human proposal-approval step is added.

## Pending owner decisions

The options below are candidates, not defaults or approvals. Ask one decision at a time; record the selected option and its consequences. New follow-up questions are needed only if a selected option leaves an implementation-blocking ambiguity.

| ID | Decision | Options to present | Affected work |
| --- | --- | --- | --- |
| D04 | Supported platforms and required native acceptance | All currently supported desktop/mobile platforms; selected named platforms; staged platform adoption with explicit unverified backlog | SDD-04, SDD-05, SDD-11, SDD-13 |
| D05 | Proportional review and emergency exceptions | Council for behavior only, ordinary review for justified no-behavior work, no emergency bypass; same with owner-approved emergency deferral; council for every change | SDD-02, SDD-09, SDD-16 |
| D06 | Existing code/spec conflicts and missing tests | Agents record gaps and preserve evidenced behavior within hard constraints; owner resolves every discrepancy; council resolves technical discrepancies with owner deciding intended product changes | SDD-03 to SDD-07 |
| D07 | Pilot selection and go/no-go authority | Agents choose bounded cases and council ratifies evidence; owner chooses and accepts each pilot; agents propose cases and owner approves selection only | SDD-15, SDD-13 |
| D08 | Performance/reliability acceptance | Council sets evidence-based thresholds from reproducible baseline; owner supplies numerical targets; observational first phase with no unsupported performance claims | SDD-07, SDD-13 |
| D09 | Missing native hardware, credentials or external evidence | Block affected acceptance and continue independent work; explicitly defer unsupported acceptance from first rollout; owner supplies prerequisites before execution | SDD-05, SDD-11 to SDD-14 |
| D10 | Integration into dev | Root may merge after current-head council/review/CI criteria; owner approves each merge; implementation stops at ready PRs | SDD-02, SDD-08, SDD-16 |
| D11 | Promotion, release and deployment | No automation beyond dev, owner handles production; agents prepare promotion/release artifacts for owner approval; explicit automatic promotion policy within existing publisher rules | SDD-11, SDD-12 |
| D12 | Enabling mandatory checks and process rollback | Owner approves activation and rollback; agent activates after agreed evidence and reports, rollback needs approval; council ratifies activation/rollback within explicit criteria | SDD-10, SDD-14 |
| D13 | Routine technical and scheduling decisions | Delegate version pin, schema details, test mapping and assignments to agents within this policy; council decides material tooling changes; owner approves each choice | SDD-01, SDD-02, all task ownership |

## Approval evidence convention

Record owner choices here in English with stable decision IDs. Carry confirmed rules into the blueprint and matching issue bodies. Council approval references its immutable proposal, evaluated revision, vote matrix and archived report. GitHub task status and implementation evidence remain separate from owner-policy approval. Do not treat silence, elapsed time or pending options as consent.
