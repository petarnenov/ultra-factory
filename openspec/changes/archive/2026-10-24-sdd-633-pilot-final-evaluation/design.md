# Design

## Scope and impact

This is a repository documentation and evidence change with no runtime behavior. The report records one bounded pilot: issue #647, PR #648, the `openspec-check-setup` capability, exact proposal/review/merge evidence, local and selected-CI checks, gaps, awkward fields, and follow-ups. It preserves the prior final #633 conditional GO ruling and its conditions; it does not authorize #620 before root verifies this PR is integrated.

## Convention corrections

The OpenSpec workflow page will show both archive forms and make spec-delta presence the decision rule. It will require a reason for `--skip-specs` in the proposal and PR. Reviewers will manually inspect the full proposal/design/tasks/spec artifacts against the semantic diff until #634 / SDD-16 enforcement exists, and record an exact-head completeness disposition. Post-archive delivery items must be complete before a final re-archive or have a justified exception and completion evidence in the PR.

The #633 change has no behavior spec delta and declares `skip_specs: true`. It must not contain a `specs/` directory. The delivering PR uses the pinned OpenSpec 1.14.3 CLI through the root npm wrapper, strictly validates this change, archives with `--skip-specs`, preserves raw command outputs, and confirms `openspec/specs/` is unchanged.

## Evidence and boundaries

The report distinguishes verified GitHub facts from earlier worker-reported local results. It includes the child branch `chore/647-openspec-setup-diagnostics`, reviewed head `c4f16dd09a31772cc86a76ba12d01e0b3e77f456`, merge `3a3fbc3eee214252147518ec49c4c1823e44e39e`, selected checks, and the exact local-validation limitations. It describes #647's unchecked archived task items without changing that archive. Issue #657 is the separate owned workflow path-selection follow-up; it is open and not integrated, and it does not block #633 or #620. No workflow or CI edit belongs in this change.

An independent reviewer must make the final manual completeness disposition on the exact final PR head. The disposable manual fixture exercise is supplemental evidence, not a replacement for that review or a retroactive claim about #647.

## Risks and rollback

A wrong archive command can omit a required behavior spec delta; a spec-free change archived without a reason can obscure intent. The explicit delta-based decision and strict archive verification mitigate both risks. A manual duty must not be described as automated enforcement. Root may revert the focused #633 PR on `dev` if the convention proves incorrect, preserving the reports and archives. No release or deployment is involved.
