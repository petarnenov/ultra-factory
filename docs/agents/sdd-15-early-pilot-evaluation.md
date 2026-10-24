# SDD-15 early-pilot evaluation

Date: 2026-10-24. This report records the #633 pilot on the development branch. It does not claim released behavior or authorize broad #620 work before the conditions below are verified.

## Outcome

**Final pilot ruling: conditional GO.** The first-available D03 Fable ruling found the bounded #647 pilot sufficient to proceed toward #620 only after #633’s convention corrections, this #633 change and archive, exact-head independent review, and root integration into `dev`. The final ruling is archived at `/Users/petarnenov/.ao/architecture-consults/archive/2026-10-24/ultra-factory-633-final-3a3fbc3-20261024T075208Z/final.md`; frozen case SHA-256 `086dc9fef214778cd4413cde9358d74f36fc61f0b3823a6db624920ce864019c`. It is one Fable D03 ruling, not council consensus. Proposal approval alone was not treated as GO.

Proposal 633-3 for these convention corrections was approved by a separate exact-revision D03 ruling. Proposal SHA-256 `bbbfb67ab1c16f5a9cf9eda105e10e68fe27068f0b7db3f0b2267edac43eb08f`; ruling archive `/Users/petarnenov/.ao/architecture-consults/archive/2026-10-24/ultra-factory-633-conventions-20261024T081204Z/final.md`. That ruling approves only this report, this change/archive, and the three named edits in `docs/agents/openspec.md`, subject to its exact conditions. Its limitations and terminal process record are preserved in the archive.

## Pilot selection and accepted capability

After #618 and #619 integrated into `dev` at `f891dd1db92b1e52a0c5997ed56a26d9e8ba1b2a` and `d6b93c51428b57d362d906f4597d4c909b238002`, respectively, #633 selected one bounded behavior change: #647, OpenSpec setup diagnostics for unsupported Node and a missing pinned CLI. #647 had one type label and one priority label and its own full proposal and ruling. No separate owner-selection gate was imposed.

- Child issue: [#647](https://github.com/petarnenov/ultra-factory/issues/647).
- Child PR: [#648](https://github.com/petarnenov/ultra-factory/pull/648), base `dev`, closes only #647 and links #633.
- Actual child branch: `chore/647-openspec-setup-diagnostics`. This differs from proposal 647-2’s provisional `feat/647-openspec-check-preflight`; the issue, bounded scope, dev base, PR target, and acceptance remained fixed.
- Child proposal: 647-2 SHA-256 `7aad8893653ac7129614578aa4b90e11783bd019397513d3f94b63234ba8df4a`; explicit Fable `claude-fable-5-1` medium D03 approval, not consensus.
- Reviewed child head: `c4f16dd09a31772cc86a76ba12d01e0b3e77f456`; native Codex review run `f5bc5825-3302-4ed6-9ccc-831da37e7d82`, zero threads. GitHub recorded the review as `COMMENTED`; its text explicitly says approved and no required changes.
- Verified merge: `3a3fbc3eee214252147518ec49c4c1823e44e39e`, 2026-10-24 07:46:21 UTC.
- Accepted capability: `openspec-check-setup`, with the `ADDED` requirement and scenarios synchronized to `openspec/specs/openspec-check-setup/spec.md`. The #618 sample stayed unarchived. #647 archived without `--skip-specs`.

## Evidence and verification

| Evidence | Result and source |
| --- | --- |
| Focused Node tests | 3 passed, 0 failed (`node --test scripts/openspec/check.test.mjs`), reported by the child and recorded in PR #648. |
| Existing OpenSpec workflow | `npm run openspec:check` exit 0; pinned 1.14.3 sample, malformed missing-scenario rejection, and repaired control behaved as expected, reported by the child. |
| Child change lifecycle | Strict validation exit 0, archive exit 0 without `--skip-specs`, synchronized spec and `openspec show` verified, `git diff --check` passed; reported by the child. |
| Policy checks | Comment suite 25/25 plus Go/scan passed; issue suite 32/32 plus issue check passed; reported by the child. |
| Composite local CI command | `make ci-check` exited 2 after policy checks because default pinned golangci-lint timed out (lint exit 4, 0 issues). This is a failure, not a pass. |
| Separate pinned lint | golangci-lint v2.14.0 with actual `--timeout=10m` exited 0 with zero issues. |
| Selected GitHub checks | `check` (comment policy), `scan` (gitleaks), and Issue tracking policy succeeded on #648. |
| Unselected checks | The Go workflow’s path filters did not select `lint` or `api-drift` for #648’s `scripts/openspec/**` and `openspec/**` paths. The local OpenSpec checks are not CI-certified on that head. Go/API workflow was not selected. |

These local outcomes are reported from the child’s preserved PR record; the final D03 reviewer did not rerun the worker’s commands. The real live #648 API, review, check, issue, and comment snapshots are retained in the #633 consultation archive under `evidence/post-ruling/`.

## Manual artifact completeness

A manual, disposable fixture exercise used the current SDD completeness criteria without invoking a validator. The incomplete proposal was explicitly **REJECTED** for missing impact/no-runtime declaration, stable requirement/scenario IDs, verification, risks, rollback, and complete tasks. A repaired control was **ACCEPTED for completeness** against the same checklist. It was not a PR-readiness or command-validation result, and it does not rewrite #647’s history. The reviewer noticed that the repaired fixture spec lists five scenario IDs while its proposal maps three; the fixture demonstrates repair of the missing sections but is not a flawless mapping example.

The worker performed that fixture review, so it does not satisfy the independent-review condition for this PR. The independent reviewer must inspect this change’s proposal, design, tasks, and no-spec-delta declaration on the exact final PR head, then record either named omissions plus repaired-head result or “no incomplete artifact found.” The PR review/body is the record for that exact head; it must include its full SHA. This review remains a manual duty until #634 / SDD-16 supplies enforcement.

## Friction, gaps, and follow-ups

- The former OpenSpec guide showed only the `--skip-specs` archive form under a tooling/documentation label, even though #647 correctly archived its new capability without the flag. The guide now selects the command by behavior spec delta, not change category.
- OpenSpec 1.14.3 archived #647 while delivery tasks 3.1 and 3.3 were unchecked. They remain unchanged in the historical archive. The CLI warned about incomplete tasks, but the raw warning was not preserved, so its wording/count cannot be independently checked. This #633 change keeps artifact tasks complete before archive; post-archive review and root integration are delivery steps, not unchecked artifact tasks.
- The #648 review state was `COMMENTED`, with approval in the text, rather than GitHub `APPROVED`. The PR body said the issue-policy check was pending while the rollup showed success. Both facts remain visible in the preserved snapshot.
- The manual fixture reviewer was also the worker author; only an independent exact-head review satisfies this PR’s disposition requirement.
- Follow-up #657 is open, labeled `chore` and `priority:high`, assigned to Petar Nenov, with coordinator29 as execution lead and worker133 assigned on `ci/657-openspec-workflow-selection`. It owns existing Go/OpenSpec workflow path selection and meaningful selection tests. It is not integrated and is not a #633 or #620 prerequisite. No workflow, CI gate, or #627 change is included here.
- #634 / SDD-16 retains automated archive-completeness enforcement. Until then the manual review duty is mandatory. No global agent integration or #620 implementation is part of this pilot.

## Requirement results

- **R633-01 / S633-01 — MET.** The bounded #647 case was selected without an owner-selection gate; the child retained its own exact proposal and ruling.
- **R633-02 / S633-02 — MET with recorded gaps.** One child issue/change/PR delivered one accepted capability, exact-head review, local validation, spec synchronization, archive, selected CI, and verified merge. The GitHub review state, aggregate lint timeout, unselected Go/API jobs, and archived task items are stated above.
- **R633-03 / S633-03 — resolved by this approved convention revision, with an independent-review condition pending on the final PR head.** The actual fixture rejection/repaired control, archive selection, exact-head disposition, post-archive task rule, #657 owner, and remaining #634 enforcement are recorded.
- **R633-04 / S633-04 — MET.** The fresh final D03 evaluation was an explicit conditional GO/NO-GO ruling with frozen case hash and process record, not a reused proposal approval.
- **R633-05 / S633-05 — MET.** The pilot stopped at `dev`; no release or deployment is claimed.

## Final integration gate

The #633 delivery PR must target `dev`, close only #633, include this report, the approved convention changes and the validated/archived `sdd-633-pilot-final-evaluation` change. It must pass exact-head independent completeness review and applicable required checks; path-unselected workflows remain gaps, not passes. Root alone integrates. **Do not assign or start #620 until root verifies this PR is integrated into `dev`.** This is not a production release, deployment, or main promotion.

## Review effort and consultation limitations

The pilot used separate first-available Fable D03 rulings for proposal 647-2, proposal 633-2, the final evidence GO/NO-GO, and this exact proposal 633-3; these were individual arbitration decisions, not three-consul consensus. PR #648 had one native Codex exact-head review with zero threads, then root integration. The final evaluation’s Fable process exited 0; its raw result, session/process identifiers, terminal status, and absence check are in the final-ruling archive. For proposal 633-3, Fable exited 0 after 150 seconds; the requested alias and medium effort are recorded, but the CLI’s canonical model field was null, so the effective model was not independently confirmed. Fable could not read the pinned checkout or compute hashes; it checked frozen policy copies and internal evidence consistency. The request also contained a truncated copy of the baseline SHA, while this proposal and the frozen case preserve the full SHA. These limitations are retained in the consultation archive. A post-ruling supplemental evidence bundle contains the actual #648 GitHub API snapshot; it does not replace or rewrite the frozen case.

## #633 delivery checks

- Node `v24.21.0`, npm `11.19.0`, and the repository-pinned OpenSpec `1.14.3` were used after installing the root lockfile with an isolated cache.
- `npm run openspec -- validate sdd-633-pilot-final-evaluation --strict --no-interactive` passed. The archive command exited 0 with `--skip-specs`; output said all tasks were complete. The archived directory has no `specs/`, and `git diff -- openspec/specs` is empty.
- `git diff --check` passed. `make ci-check ARGS="--only comment,issue"` passed the 25 comment-policy tests, `go test ./cmd/commentpolicy`, comment check, and 32 issue-policy tests. Before a PR existed, the wrapper explicitly skipped the live issue-policy check; that check must be rerun after PR creation.
- The gitleaks CLI is unavailable on this host. The draft PR’s remote `scan` check is required evidence; it is not claimed locally.
- The first OpenSpec version query found no local install and resolved a global 1.14.1 binary through the npm script. It was discarded as invalid evidence; no project command used that binary. Installing the root lockfile then verified and used 1.14.3. The default npm cache also failed with an `EPERM` caused by root-owned entries; no ownership or permissions were changed, and installs succeeded with an isolated `/private/tmp` cache. A first comment/issue policy run hit the default Go cache sandbox denial; the rerun used isolated Go caches and passed.
- The frontend lockfile install used `--ignore-scripts` only to supply the TypeScript compiler required by the comment-policy check. It changed no tracked files. npm reported 53 existing dependency advisories; no dependency updates or audit fixes were made.
