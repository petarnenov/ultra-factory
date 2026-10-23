# Tasks

- [x] 1. Translate D00–D13 into `docs/agents/sdd-policy.md`, including roles, readiness/completion, D03 failure cases, source conflict resolution, and feature/bugfix/maintenance walkthroughs.
- [x] 2. Add an `AGENTS.md` pointer and update `docs/documentation-map.md` with the SDD policy and concern-specific source-of-truth/synchronization rules.
- [x] 3. Update feature and bug issue forms to capture type/priority intent and proposal/change context without claiming labels are applied automatically.
- [x] 4. Update the PR template to require the single issue, full proposal/change ID, impact declaration, semantic diff assessment, reviewed head, verification evidence, and dev-versus-release distinction.
- [x] 5. Validate the OpenSpec change, documentation links/claims, issue-form YAML, PR template, and walkthroughs; record exact environment gaps and coordinate any heavy validation. Heavy `make ci-check` remains unrun until worker 117 releases the shared serial validation window.
- [x] 6. Reconcile the final diff with approved proposal 619-1 and its binding conditions; archive this #619 change in the same PR, preserving the separate #618 bootstrap change.
- [ ] 7. Provide root-ready final-head evidence after independent review and applicable required CI succeed on the same head; do not merge or close the issue.
