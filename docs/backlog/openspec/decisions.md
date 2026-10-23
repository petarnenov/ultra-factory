# Owner decisions for machine implementation

Status: owner interview in progress. Do not schedule implementation from this backlog until the pending decisions below are resolved and reconciled into the blueprint and issue acceptance criteria. This record authorizes planning changes only; it does not start implementation, merge, release or deployment.

## Confirmed decisions

### D00 Exclude AO Cloud

Owner instruction: AO Cloud will not be used in this fork. Exclude both its public control plane and private submodule, tenant/billing/PostgreSQL behavior, cloud-only contracts/client behavior, deployment and private-owner evidence from this SDD adoption. Existing files are not deleted and existing CI checks are not removed. Shared code used by supported local or Railway surfaces remains in scope for those consumers only.

### D01 Retain Railway and self hosted remote workers

Owner answer: Yes to retaining Railway/self-hosted, hub/BFF and remote workers while excluding AO Cloud. Preserve their security, operational and recovery scenarios. This does not authorize starting the stopped Railway hub or deploying anything.

### D02 Proposal approval by geo.architect

Owner answer: Ask geo.architect. All change proposals use the repository's `.agents/skills/geo.architect/SKILL.md` before implementation. Use its independent analysis, mutual critique, proposal identity and archived evidence. Normal council ratification uses its voting rules; D03 overrides its requirement for all three successful ballots and unresolved-decision escalation when the council cannot decide or a consul is unavailable. A completed consultation does not grant merge, release, spending or deployment authority. D05 requires the full proposal process for every change, including documentation and formatting.

Changed constraints or proposal content invalidate affected approvals and require review of the new version. This selects the future approval mechanism; no council has been launched or approval claimed during this interview.

### D03 Final decision authority and fallback order

Owner instruction: Fable 5.1 decides conclusively; if unavailable, Codex Astra decides; if also unavailable, OpenCode DeepSeek Flash 4.1 decides. For an unresolved council decision or an unavailable consul, obtain an explicit final ruling on the exact proposal/revision from the first available consul in this order:

1. Claude Code, `claude-fable-5-1`, medium effort.
2. Codex, `gpt-6-astra`, medium effort.
3. OpenCode, `opencode/deepseek-v4.1-flash`, provider default effort.

The selected consul is the final arbiter, not an extra majority vote. Preserve available analyses, dissent, the reason a higher-priority consul is unavailable, the selected arbiter and its explicit decision with rationale in the consultation archive. An observed provider/harness/access failure can establish unavailability; silence or an observation timeout alone is not a decision or proof that a live consultation ended. Do not substitute another model.

This owner-approved project policy overrides the skill's all-three-ballots requirement and unresolved-council human escalation for this initiative. The arbiter must resolve documented objections within approved scope and existing hard requirements; a decision does not itself grant missing credentials or authority to deploy, spend, merge or expand product scope. If none of the three can provide a ruling, no approval exists: keep the affected task blocked and report the missing prerequisite. Independent already-approved work can continue. No automatic human proposal-approval step is added.

### D04 All supported platforms

Owner answer: All. Include macOS, Windows, Linux, iOS, Android and Railway web. Each supported surface needs capability specifications and evidence from its actual platform where native behavior is involved. Renderer mocks, typechecks or another operating system are not substitutes for native acceptance. An unavailable platform check is not a pass; D09 governs missing hardware/access without treating an unrun check as successful. Platform support is not deferred out of the initiative.

### D05 Full proposal for every change

Owner answer: Full proposal for everything. Every future implementation change, including documentation, formatting, dependencies, tooling, CI, maintenance, automation, promotion and emergency fixes, requires a full proposal and geo.architect review under D02/D03 before execution. A no-behavior classification explains impact; it never exempts the change from proposal, review or required artifacts. No emergency bypass or deferred proposal is authorized.

Each change records why, scope/non-goals, impact, affected capabilities or a justified no-runtime-behavior statement, acceptance scenarios, technical approach, tasks, verification and rollback/applicability. For a text-only change, scenarios can verify documentation correctness; do not invent a runtime behavior change to satisfy the process. The pinned schema may represent no runtime delta explicitly, but the proposal and council decision are still mandatory. Record human policy choices during this planning interview without treating that recording as authorization to implement the initiative.

### D06 Resolve existing contradictions through geo.architect

Owner answer: geo.architect. When code, tests and documentation disagree, the council evaluates the evidence and establishes the intended in-scope behavior, using D03 final arbitration if needed. Neither existing code nor prose wins automatically. Record the contradiction, evidence, decision and any corrective implementation in the proposal; retain known defects as explicit gaps until resolved. Do not silently weaken hard requirements or expand the fork scope. Spec changes and implementation fixes follow the same full proposal and verification process.

### D07 Agents select and execute pilots

Owner answer: Agents select and execute (option 3). Agents choose bounded real tasks for the early and broad pilots and execute within approved scope. Every selected change still requires its full proposal and council approval under D05 before implementation. No additional owner approval of pilot selection is required. geo.architect assesses the final evidence and decides go/no-go under D02/D03; selection authority is not permission to skip required validation or declare success without evidence.

### D08 Measure and accumulate results first

Owner answer: In the first stage, measure and accumulate results. During the pilots and initial rollout measurement window, collect reproducible performance/reliability observations with workload, platform, environment, revision and failures retained. Do not introduce new numerical performance/SLO thresholds as blocking SDD adoption criteria. Existing correctness, security, reliability contracts and already-required checks remain enforced; observation mode does not turn a functional failure into a pass.

Threshold selection is later work based on the collected evidence and a new fully reviewed proposal; it is not a prerequisite for this initiative's first rollout. Report distributions and sample counts without inventing an acceptance SLO or claiming an unmeasured target was achieved.

### D09 Continue executable work when evidence is unavailable

Owner answer: Continue (repeated after clarification). Missing hardware, credentials or external services must not stop unrelated implementation or the executable portions of rollout. Record the exact missing prerequisite, affected platform/scenario and an owned follow-up issue with verification steps. Continue independent approved work; mark only the affected acceptance as blocked/unverified.

Do not fabricate a pass, remove a platform from scope, weaken security/functional gates or declare full verified adoption while required evidence remains missing. Report delivered/verified portions separately from outstanding platform checks. This policy permits continued execution, not certification of unavailable evidence; no separate waiver of full-coverage acceptance was given. The first-stage performance observation policy D08 is independent of missing native or functional verification.

### D10 Autonomous root integration into dev

Owner answer: Yes. The project root orchestrator may merge implementation PRs into dev without a per-PR owner confirmation after the full proposal is approved, independent review approves the exact final head, all applicable required CI succeeds on that head, and no unresolved blocking review remains. Re-review and revalidate after head changes. Workers and coordinators cannot merge. D10 does not authorize promotion to main, publishing or deployment; D11 governs those boundaries. Closing a child issue requires verified integration and its complete acceptance evidence, not merely a green draft PR.

### D11 Automation stops at dev

Owner answer: Stop at dev. Promotion to main, publication, release execution and deployment are entirely owner-operated. Agents do not merge into main, start promotion/release/deployment workflows, or automatically create a promotion PR under this initiative. They may implement the requested release/operations documentation and prepare traceable evidence within their approved dev tasks. Preserve the sole-publisher and existing release contracts. This rule does not remove all-platform readiness verification from scope and never permits production publication as a test.

## Pending owner decisions

The options below are candidates, not defaults or approvals. Ask one decision at a time; record the selected option and its consequences. New follow-up questions are needed only if a selected option leaves an implementation-blocking ambiguity.

| ID | Decision | Options to present | Affected work |
| --- | --- | --- | --- |
| D12 | Enabling mandatory checks and process rollback | Owner approves activation and rollback; agent activates after agreed evidence and reports, rollback needs approval; council ratifies activation/rollback within explicit criteria | SDD-10, SDD-14 |
| D13 | Routine technical and scheduling decisions | Delegate version pin, schema details, test mapping and assignments to agents within this policy; council decides material tooling changes; owner approves each choice | SDD-01, SDD-02, all task ownership |

## Approval evidence convention

Record owner choices here in English with stable decision IDs. Carry confirmed rules into the blueprint and matching issue bodies. Council approval references its immutable proposal, evaluated revision, vote matrix and archived report. GitHub task status and implementation evidence remain separate from owner-policy approval. Do not treat silence, elapsed time or pending options as consent.
