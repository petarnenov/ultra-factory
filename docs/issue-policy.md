# Issue tracking policy

Every change starts as a GitHub issue. The issue carries **exactly one type
label** and **exactly one priority label**, and the pull request that makes the
change references it. A pull request check enforces this before the change can
land.

This is the rule for agents and people alike. Backlog labels (`rw*`, `co*`,
`status:*`, `wave:*`, `host:*`, `needs-human`, `good first issue`, and so on)
are additive: they do not replace and do not satisfy the type or priority
requirement.

## Type labels

One label per issue, matching the work:

| Label           | Use for                                                              | Example                                                       |
| --------------- | -------------------------------------------------------------------- | ------------------------------------------------------------- |
| `bug`           | Behavior that is wrong, crashes, or blocks an existing capability.   | A spawn fails when the explicit harness belongs to another one. |
| `enhancement`   | New capability or a deliberate change in product behavior.           | Bind a GitHub account to a project.                            |
| `documentation` | Docs, ADRs, runbooks, and contributor guides.                        | Document the coordinator rollout and rollback.                 |
| `accessibility` | A barrier affecting people with disabilities.                        | A menu item is unreachable by keyboard.                        |
| `chore`         | Maintenance with no product behavior change: CI, tooling, dependency bumps, measurements, refactors. | Record the per-process resource baseline. |

There is no separate `refactor` label: a refactor takes `chore`, or the type of
the behavior it changes. A smaller taxonomy that people apply is better than a
complete one nobody uses.

## Priority labels

One label per issue, assigned by the agent that files the issue. A person filing
an issue assigns it too; the issue forms only prefill the type label, so a
maintainer or agent adds the priority during triage. The orchestrator may change
a priority and says why in an issue comment.

| Label               | Means                                                              | Example                                             |
| ------------------- | ------------------------------------------------------------------ | --------------------------------------------------- |
| `priority:critical` | Production is down, data is at risk, or security is exposed.        | The hub stays down and every route returns `502`.    |
| `priority:high`     | Blocks a shipped capability or the current batch.                   | A spawn with an explicit harness fails at the chat controller. |
| `priority:medium`   | Normal work. The default for a newly filed issue.                   | Bind a GitHub account to a project.                 |
| `priority:low`      | Nice to have; nothing shipped is blocked.                           | A feature request with no current owner.            |

The label sets live in the issue tracker. Create them in a fork with:

```bash
gh label create chore --description "Maintenance, tooling, CI, or dependency work" --color "fef2c0"
gh label create priority:critical --description "Production down, data loss, or security exposure" --color "b60205"
gh label create priority:high --description "Blocks a shipped capability or the current batch" --color "d93f0b"
gh label create priority:medium --description "Normal work; the default for a filed issue" --color "fbca04"
gh label create priority:low --description "Nice to have; nothing shipped is blocked" --color "c2e0c6"
```

The `bug`, `enhancement`, `documentation`, and `accessibility` labels already
exist. Do not rename them.

## Enforcement

[`.github/workflows/issue-policy.yml`](../.github/workflows/issue-policy.yml)
runs [`scripts/issue-policy/check.mjs`](../scripts/issue-policy/check.mjs) on
every pull request. It fails when:

- the title and description reference no issue at all;
- a referenced issue has no type label or no priority label;
- a referenced issue carries more than one type label or more than one priority
  label.

A plain `#123` counts. `Closes #123` is the recommended form because GitHub
then links the pull request and closes the issue on merge. Write the reference
in plain text: the check treats fenced blocks and inline code as literal text,
so an unfilled placeholder or a quoted failure message does not become a
reference. It also ignores references to other pull requests and to other
repositories (`owner/repo#123`). A full issue URL
(`https://github.com/<owner>/<repo>/issues/123`) is not recognised either, so
write `Closes #123` or at least `#123`.

The failure names what is missing and prints the fix, for example:

```text
#412 references no issue: add "Closes #123" to the description
#412 references #123, which has no priority label: gh issue edit 123 --add-label priority:medium
```

The workflow runs when the pull request is opened, edited, reopened,
synchronized, or marked ready for review. Labeling the issue does not trigger
it. After fixing the labels, re-run the failed check from the Actions tab
(**Re-run jobs**, or `gh run rerun <run-id>`); the script reads labels live, so
the new run sees them. Editing the title or description also starts a fresh
run.

A pull request check only blocks a merge when branch protection requires it.
A maintainer must add **Issue tracking policy** to the required status checks
for `dev` and `main` in the repository settings.

## Exemptions

There are exactly two, both matched on branch names in the check and both
narrow:

| Exemption          | Matches                                       | Reason                                                                 |
| ------------------ | --------------------------------------------- | ---------------------------------------------------------------------- |
| Promotion          | head `dev`, base `main`                        | The promotion pull request is the batch record, not a single change.   |
| Agent pin refresh  | head `chore/agent-cli-pins`                    | The weekly agent-pin workflow owns that change and its record.         |

Both exemptions also require the head branch to live in this repository. A
fork branch named `dev` or `chore/agent-cli-pins` is still checked, as is a
feature pull request that targets `main` instead of `dev`. Nothing else is
exempt by default. If a path turns out to be genuinely impossible to check,
document the evidence here and narrow the exemption rather than widening it.

## Pull requests

- Reference the issue in the description with `Closes #NNN`.
- One issue per pull request; split separate work into separate pull requests.
- Keep exactly one type and one priority on the issue. The check reports
  duplicates as failures, so fix the issue rather than the pull request.

Local run, against a real repository:

```bash
make ci-check ARGS="--only issue"
```

That command resolves the same inputs the workflow sets: `GITHUB_REPOSITORY`
from `origin`, `PR_NUMBER` from the current branch's pull request, and
`GH_TOKEN` from the environment or `gh auth token`. The check reads the live
title, body, and labels, so it needs an open pull request and GitHub API
access; before the pull request exists there is nothing to check. The
equivalent manual invocation is:

```bash
GH_TOKEN=$(gh auth token) GITHUB_REPOSITORY=owner/repo \
  node scripts/issue-policy/check.mjs --pr 412
```
