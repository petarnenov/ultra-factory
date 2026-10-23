# OpenSpec adoption backlog

Tracker: [#617](https://github.com/petarnenov/ultra-factory/issues/617). [Blueprint and repository audit](blueprint.md).

Status: [owner decisions D00–D13](decisions.md) are complete and reconciled. Ready for an assigned machine implementation run; this planning delivery has not started one. GitHub issues are authoritative for live status; this table is the reviewed planning snapshot on 2026-10-23. No assignees or dates are invented. Role owners are in the task bodies; the scheduler assigns people or sessions when work starts.

High priority marks a foundation or verification gate blocking this adoption batch. Medium marks normal migration work. No item is a production incident, so none is critical. Read dependencies before sorting by priority.

| Task | Issue | Priority | Hard dependencies | Initial status |
| --- | --- | --- | --- | --- |
| [SDD-01 Initialize OpenSpec latest with a reproducible repository workflow](tasks/SDD-01.md) | [#618](https://github.com/petarnenov/ultra-factory/issues/618) | high | None | Ready for assigned implementation |
| [SDD-02 Define SDD intake, review and source of truth policy](tasks/SDD-02.md) | [#619](https://github.com/petarnenov/ultra-factory/issues/619) | high | #618 | Waiting for dependencies |
| [SDD-15 Run an early SDD pilot on one bounded capability](tasks/SDD-15.md) | [#633](https://github.com/petarnenov/ultra-factory/issues/633) | high | #619 | Waiting for dependencies |
| [SDD-03 Baseline daemon, CLI, lifecycle and storage capabilities](tasks/SDD-03.md) | [#620](https://github.com/petarnenov/ultra-factory/issues/620) | high | #618, #619, #633 | Waiting for dependencies |
| [SDD-04 Baseline desktop and web behavior and accessibility](tasks/SDD-04.md) | [#621](https://github.com/petarnenov/ultra-factory/issues/621) | medium | #619, #620 | Waiting for dependencies |
| [SDD-05 Baseline mobile contracts and device verification](tasks/SDD-05.md) | [#622](https://github.com/petarnenov/ultra-factory/issues/622) | medium | #619, #620 | Waiting for dependencies |
| [SDD-06 Baseline Railway, self hosted workers and supported shared contracts](tasks/SDD-06.md) | [#623](https://github.com/petarnenov/ultra-factory/issues/623) | medium | #619, #620 | Waiting for dependencies |
| [SDD-07 Define security, privacy and nonfunctional scenarios](tasks/SDD-07.md) | [#624](https://github.com/petarnenov/ultra-factory/issues/624) | high | #619, #620 | Waiting for dependencies |
| [SDD-08 Integrate repository-local specification context with agent assignments](tasks/SDD-08.md) | [#625](https://github.com/petarnenov/ultra-factory/issues/625) | medium | #619, #620 | Waiting for dependencies |
| [SDD-09 Add requirement and scenario evidence validation](tasks/SDD-09.md) | [#626](https://github.com/petarnenov/ultra-factory/issues/626) | high | #619, #620 | Waiting for dependencies |
| [SDD-16 Enforce archive completeness independently of OpenSpec](tasks/SDD-16.md) | [#634](https://github.com/petarnenov/ultra-factory/issues/634) | high | #626 | Waiting for dependencies |
| [SDD-10 Integrate OpenSpec and traceability checks into CI](tasks/SDD-10.md) | [#627](https://github.com/petarnenov/ultra-factory/issues/627) | high | #618, #624, #626, #634 | Waiting for dependencies |
| [SDD-11 Connect release and distribution evidence to specifications](tasks/SDD-11.md) | [#628](https://github.com/petarnenov/ultra-factory/issues/628) | medium | #620, #621, #622, #623, #627 | Waiting for dependencies |
| [SDD-12 Connect operations, incidents and deprecation to specifications](tasks/SDD-12.md) | [#629](https://github.com/petarnenov/ultra-factory/issues/629) | medium | #623, #624, #626 | Waiting for dependencies |
| [SDD-13 Run the broad SDD pilot across feature, bugfix and operations](tasks/SDD-13.md) | [#630](https://github.com/petarnenov/ultra-factory/issues/630) | medium | #621, #622, #623, #624, #625, #627, #628, #629, #633, #634 | Waiting for dependencies |
| [SDD-14 Roll out required SDD gates and measure adoption](tasks/SDD-14.md) | [#631](https://github.com/petarnenov/ultra-factory/issues/631) | medium | #630 | Waiting for dependencies |

## Scheduling and ownership

Start SDD-01 → SDD-02 → SDD-15 (early bounded pilot, #633) → SDD-03. The early pilot uses geo.architect proposal approval and reviewer-led evidence checks and a minimal capability seed; its report and convention corrections must land before broad baselining. Task numbers are stable identifiers, not execution order.

After SDD-03, capability owners may develop separate specs without overlapping file ownership. SDD-09 → SDD-16 (archive enforcement, #634); SDD-10 waits for SDD-07, SDD-09 and SDD-16. Release and operations evidence precede the broad SDD-13 pilot; SDD-14 enables required enforcement afterward. Keep heavy validation serial on shared hosts.

Each independently mergeable implementation issue/PR owns one OpenSpec change. Umbrella trackers link children without a shared delta. Pilot issues own evaluation reports and link separate selected implementation issues. Agent guidance stays repository-local; global AO prompts and embedded skills are outside this initiative. CI validates declared impact and references; current-head semantic review checks omitted behavior.

Every task needs a named owner, PR, reviewed head, CI evidence and dev merge SHA before completion. The tracker stays open until SDD-14's rollout evidence is accepted. Do not treat a closed issue alone as proof: verify the merge and all acceptance criteria. No production deployment or agent sessions are launched by this planning change.

## Verification of this planning delivery

Verify all 16 issue bodies against their local task mirrors, one type and priority label per issue, tracker membership and dependency numbers. Verify relative documentation links, a dependency DAG without cycles, `git diff --check`, and the published branch SHA. These are documentation checks, not runtime or OpenSpec implementation validation.

## Machine handoff

1. Read [owner decisions](decisions.md), the blueprint, repository AGENTS.md and the current GitHub task body. The owner record governs this initiative; do not treat historical planning defaults as a second policy.
2. Root assigns eligible worker machines and accountable owners under the repository orchestration rules. Start with #618, then #619 → #633 → #620; enforce the dependency graph for the remaining tasks. No worker/coordinator merges.
3. For every change, including bootstrap, documentation and tooling, author the full proposal before execution and obtain geo.architect approval. Bootstrap may author Markdown before OpenSpec is installed. Apply D03 arbitration: Fable 5.1 → Codex Astra → OpenCode DeepSeek Flash 4.1; preserve explicit rulings, identity and archive evidence. No available arbiter means the affected proposal is not approved.
4. #618 resolves OpenSpec latest at setup and locks the resolved version for reproducible checks. Agents propose remaining technical choices; council decisions and root scheduling follow the confirmed policy without another owner design interview.
5. Keep AO Cloud excluded, Railway/self-hosted included and all supported platforms accounted for. Missing hardware/access creates owned verification follow-ups; independent work continues. Record performance observations without new numerical blocking SLOs in the first stage.
6. Implement, run the complete applicable checks and obtain independent review on the final head. Root may merge qualifying PRs only into dev. Preserve incomplete acceptance as incomplete even if another part is delivered.
7. Root enables required SDD checks after pilot/readiness evidence and target-branch workflow availability. Downgrading them requires explicit owner permission. Promotion/main/release/deployment remains owner-operated; do not launch those workflows as tests.
8. Record each child issue's evidence, head and dev integration in #617. Maintain the distinction between ready policy, implemented process, verified platform coverage and owner-operated release. Close the initiative only when its full acceptance evidence exists.
