# Issue tracking policy check

`scripts/issue-policy/check.mjs` enforces the policy in
[`docs/issue-policy.md`](../../docs/issue-policy.md): a pull request must
reference an issue, and every referenced issue must carry exactly one type label
and exactly one priority label.

The check reads the pull request title and description, collects `#NNN`
references outside fenced code blocks, and ignores references to other pull
requests and to other repositories. It prints one actionable line per failure,
including the `gh issue edit` command that fixes it.

Two pull requests are exempt, by head and base branch only:

- promotion of `dev` into `main`: the batch record, not a single change;
- the automated agent-pin refresh on `chore/agent-cli-pins`.

Run the tests and the check locally:

```bash
npm run issue:test
GH_TOKEN=$(gh auth token) GITHUB_REPOSITORY=owner/repo npm run issue:check -- --pr 412
```

The GitHub Actions workflow is
[`.github/workflows/issue-policy.yml`](../../.github/workflows/issue-policy.yml).
It reruns when the pull request is opened, edited, reopened, synchronized, or
marked ready for review. Label changes on the issue do not trigger it, so after
labelling the issue, edit the pull request or rerun the workflow from the
Actions tab. The workflow needs `issues: read`; fork pull requests on a public
repository are checked normally.
