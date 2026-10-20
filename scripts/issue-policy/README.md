# Issue tracking policy check

`scripts/issue-policy/check.mjs` enforces the policy in
[`docs/issue-policy.md`](../../docs/issue-policy.md): a pull request must
reference an issue, and every referenced issue must carry exactly one type label
and exactly one priority label.

The check reads the pull request title and description, collects `#NNN`
references outside fenced blocks and inline code, and ignores references to
other pull requests and to other repositories. Full issue URLs are not
recognised; write `#123`. It prints one actionable line per failure, including
the `gh issue edit` command that fixes it.

Two same-repository pull requests are exempt, by head and base branch only;
a fork branch carrying one of those names is not exempt:

- promotion of `dev` into `main`: the batch record, not a single change;
- the automated agent-pin refresh on `chore/agent-cli-pins`.

Run the tests and the check locally with the inputs CI uses:

```bash
make ci-check ARGS="--only issue"            # current branch's pull request
make ci-check ARGS="--only issue --pr 412"   # a specific pull request
```

The wrapper sets the variables the workflow sets: `GH_TOKEN`,
`GITHUB_REPOSITORY`, and `PR_NUMBER`. It resolves the repository from `origin`,
the pull request from the current branch (or `--pr`), and the token from
`GH_TOKEN`, `GITHUB_TOKEN`, or `gh auth token`. The check reads the live title,
body, and issue labels through the GitHub API, so it needs an open pull request
and network access. Before the pull request exists there is nothing to check;
run it again after opening the PR.

The equivalent manual invocation is:

```bash
npm run issue:test
GH_TOKEN=$(gh auth token) GITHUB_REPOSITORY=owner/repo node scripts/issue-policy/check.mjs --pr 412
```

The GitHub Actions workflow is
[`.github/workflows/issue-policy.yml`](../../.github/workflows/issue-policy.yml).
It runs when the pull request is opened, edited, reopened, synchronized, or
marked ready for review. Label changes on the issue do not trigger it: after
labelling, re-run the failed pull request run from the Actions tab (Re-run
jobs, or `gh run rerun <run-id>`), because the script reads labels live, or
edit the description. The workflow needs `issues: read`; fork pull requests on
a public repository are checked normally.
