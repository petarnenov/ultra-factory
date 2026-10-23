# OpenSpec adoption backlog

Tracker: [#617](https://github.com/petarnenov/ultra-factory/issues/617). [Blueprint and repository audit](blueprint.md).

Status: planned; implementation has not started. GitHub issues are authoritative for live status; this table is the creation snapshot on 2026-10-23. No assignees or dates are invented. Role owners are in the task bodies; the scheduler assigns people or sessions when work starts.

High priority marks a foundation or verification gate blocking this adoption batch. Medium marks normal migration work. No item is a production incident, so none is critical. Read dependencies before sorting by priority.

| Task | Issue | Priority | Hard dependencies | Initial status |
| --- | --- | --- | --- | --- |
| [SDD-01 Pin OpenSpec and initialize the repository workflow](tasks/SDD-01.md) | [#618](https://github.com/petarnenov/ultra-factory/issues/618) | high | None | Ready for scheduling |
| [SDD-02 Define SDD intake, review and source of truth policy](tasks/SDD-02.md) | [#619](https://github.com/petarnenov/ultra-factory/issues/619) | high | #618 | Waiting for dependencies |
| [SDD-03 Baseline daemon, CLI, lifecycle and storage capabilities](tasks/SDD-03.md) | [#620](https://github.com/petarnenov/ultra-factory/issues/620) | high | #618, #619 | Waiting for dependencies |
| [SDD-04 Baseline desktop and web behavior and accessibility](tasks/SDD-04.md) | [#621](https://github.com/petarnenov/ultra-factory/issues/621) | medium | #619, #620 | Waiting for dependencies |
| [SDD-05 Baseline mobile contracts and device verification](tasks/SDD-05.md) | [#622](https://github.com/petarnenov/ultra-factory/issues/622) | medium | #619, #620 | Waiting for dependencies |
| [SDD-06 Baseline public cloud, shared contracts and Railway](tasks/SDD-06.md) | [#623](https://github.com/petarnenov/ultra-factory/issues/623) | medium | #619, #620 | Waiting for dependencies |
| [SDD-07 Define security, privacy and nonfunctional scenarios](tasks/SDD-07.md) | [#624](https://github.com/petarnenov/ultra-factory/issues/624) | high | #619, #620 | Waiting for dependencies |
| [SDD-08 Integrate specification context with agent assignments and review](tasks/SDD-08.md) | [#625](https://github.com/petarnenov/ultra-factory/issues/625) | medium | #619, #620 | Waiting for dependencies |
| [SDD-09 Add requirement and scenario evidence validation](tasks/SDD-09.md) | [#626](https://github.com/petarnenov/ultra-factory/issues/626) | high | #619, #620 | Waiting for dependencies |
| [SDD-10 Integrate OpenSpec and traceability checks into CI](tasks/SDD-10.md) | [#627](https://github.com/petarnenov/ultra-factory/issues/627) | high | #618, #624, #626 | Waiting for dependencies |
| [SDD-11 Connect release and distribution evidence to specifications](tasks/SDD-11.md) | [#628](https://github.com/petarnenov/ultra-factory/issues/628) | medium | #620, #621, #622, #623, #627 | Waiting for dependencies |
| [SDD-12 Connect operations, incidents and deprecation to specifications](tasks/SDD-12.md) | [#629](https://github.com/petarnenov/ultra-factory/issues/629) | medium | #623, #624, #626 | Waiting for dependencies |
| [SDD-13 Pilot SDD on a feature, bugfix and operational change](tasks/SDD-13.md) | [#630](https://github.com/petarnenov/ultra-factory/issues/630) | medium | #621, #622, #623, #624, #625, #627, #628, #629 | Waiting for dependencies |
| [SDD-14 Roll out required SDD gates and measure adoption](tasks/SDD-14.md) | [#631](https://github.com/petarnenov/ultra-factory/issues/631) | medium | #630 | Waiting for dependencies |

## Scheduling and ownership

Start SDD-01, then SDD-02, then SDD-03. After those foundations, capability owners may develop separate specs without overlapping file ownership. SDD-09 follows SDD-03; SDD-10 waits for SDD-07 and SDD-09. Release and operations evidence precede the pilot; required enforcement follows the pilot. Keep heavy validation serial on shared hosts.

Every task needs a named owner, PR, reviewed head, CI evidence and dev merge SHA before completion. The tracker stays open until SDD-14's rollout evidence is accepted. Do not treat a closed issue alone as proof: verify the merge and all acceptance criteria. No production deployment or agent sessions are launched by this planning change.

## Verification of this planning delivery

Verify all 14 issue bodies against their local task mirrors, one type and priority label per issue, tracker membership and dependency numbers. Verify relative documentation links, a dependency DAG without cycles, `git diff --check`, and the published branch SHA. These are documentation checks, not runtime or OpenSpec implementation validation.
