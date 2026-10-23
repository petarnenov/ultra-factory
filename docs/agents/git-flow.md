# Git flow

Loaded on demand from [`AGENTS.md`](../../AGENTS.md). Read it before branching, merging, or promoting `dev` to `main`.

`main` is the release boundary and the only branch that publishes. `dev` is the integration branch. Every story, bug, or improvement reaches `main` through `dev`.

1. **Task worktree branched from `dev`.** One worktree and one branch per task:
   ```bash
   git fetch origin
   git worktree add ../uf-worktrees/<slug> -b feat/<slug> origin/dev
   ```
   Branch from `dev`, never from `main`. Never work in the invoking working tree: it may hold unrelated uncommitted work. Keep the worktree outside the checkout (an untracked `.worktrees/` directory inside it gets walked by repo tooling), and never use `~/.ao/data/worktrees/...`, which the AO product itself creates for agent sessions.
2. **Open the PR against `dev`.** One issue per PR. Keep the repo's conventional prefixes (`feat/`, `fix/`, `docs/`, `test/`, `chore/`, `ci/`).
3. **Merge the task branch into `dev`** once the full CI job set is green on its final commit. Green CI alone is not the promotion bar.
4. **Promote `dev` to `main`** when the batch is ready: every CI job green on the latest `dev` commit, plus a manual smoke check of the touched surface (for UI, `ao preview` or the desktop lab). The promotion is a PR into `main`, so it is also where the `main`-only checks run for the batch. `dev` never publishes on its own; the `main`-only workflows (`hub-image`, `worker-image`, `deploy-docs`, the release guards) stay the only path to a release.
5. **Sync `main` back into `dev` immediately after promoting.** `version-bump` commits a semver bump and a CHANGELOG entry to `main` when the promoted batch touches release inputs, so `dev` is usually stale the moment the promotion lands; a docs-only batch gets no bump, so pass `--skip-wait`. `make promote-sync` does this: it waits for the bump commit, merges `main` into `dev`, and pushes `dev`. Merge `main` into `dev` before starting the next task, or the next promotion conflicts in `frontend/package.json` and `CHANGELOG.md`.
6. **Clean up the worktree** after the merge (`git worktree remove <dir>`, then delete the branch). Never force-remove a dirty worktree.

`pricing-catalog-validate.yml` only runs on pull requests whose base is `main`, so a task PR touching `pricing/**` is first validated at promotion; run that job locally in the task worktree instead of waiting.
