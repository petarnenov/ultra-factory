# KG-2.1.1 · Task: Record changed files and show them on similar tasks

**Status:** ⬜ todo — [#557](https://github.com/petarnenov/ultra-factory/issues/557)

**Type:** enhancement (daemon). **Estimate:** 1 pd. **Story:** KG-2.1.
**Order:** 4 of 8. **Depends on:** none (KG-1.1.1 (#553) adds the `Item` PR field this
task reuses). **Next:** KG-3.1.1 (#560).

## Goal

Store the paths a task changed and render the PR plus the main paths on
"Similar past tasks".

## Context (verified 2026-10-23)

- No code records changed paths; `PRFacts.ChangedFiles` is a count only
  (`backend/internal/domain/pr.go`, filled by
  `adapters/scm/github/observer_provider.go`).
- `domain.Session` has `WorkspacePath` and `Branch`; `PRFacts.TargetBranch`
  names the base. Each daemon records only its own workers, so the worktree
  is local to the recording daemon.
- The worktree can be destroyed when the session ends
  (`session_manager/manager.go`, `m.workspace.Destroy`), so capture must
  happen on worker reports, not only on termination.
- Task embedding text is `t.title + '\n' + t.summary`
  (`graph.go` `MissingEmbeddings`); a report clears `t.embedding`, which
  re-embeds the task.

## Subtasks

- [ ] Add a port in `knowledgegraph` (e.g. `ChangedPaths(ctx, sess)`)
  implemented in the daemon with
  `git -C <WorkspacePath> diff --name-only <merge-base of origin/<target>>...HEAD`;
  return nothing when the worktree is missing. Keep git out of `knowledgegraph`.
- [ ] On a report job, store `t.files` (at most 50 paths). Leave the stored
  value when the worktree is gone.
- [ ] Return `t.prs` and `t.files` from `tasksCypher` and `tasksVectorCypher`;
  render the PR and the 5 most significant paths (most-shared directory
  prefixes first; the selection rule lives in one tested function).
- [ ] Append the paths to the task embedding text.
- [ ] Unit tests for path selection, rendering and the missing-worktree case.

## Acceptance criteria

- [ ] A merged task with a done report has `t.files`.
- [ ] Its line under "Similar past tasks" shows `PR: <url> · files: a, b, c`.
- [ ] No git process runs on the spawn path.

## Verification

Unit tests; live test; one probe whose prompt paraphrases a recent merged
task shows its PR and paths.

## Rollout

Daemon change; part of the shared rollout.
