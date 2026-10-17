# RW-5.4.1 · Task: Agent credentials for orchestrators in the hub
**Status:** ⬜ todo

**Type:** operations + documentation. **Estimate:** 0.5 pd. **Story:** RW-5.4.

## Goal
An orchestrator started in the hub (claude-code or codex) is authenticated without any secret in the repo, the image or a log.

## Context
- The hub image is built FROM the worker image, which already contains `claude`, `codex` and `opencode` (`deploy/railway/worker/Dockerfile`, `AGENT_NPM_PACKAGES`).
- The `ultra-factory` worker service receives `ANTHROPIC_API_KEY` and `GH_TOKEN` as Railway variables. The `hub` service has neither today.
- `HOME` is `/data/home` on the volume, so an interactive provider login persists across redeploys.
- The orchestrator merges with `gh pr merge` and comments on issues, so it needs a GitHub token with the matching scope.

## Subtasks
- [ ] Decide per harness: API key as a Railway variable (`ANTHROPIC_API_KEY`, `OPENAI_API_KEY`), or a one-time interactive login through the hub terminal persisted under `/data/home`. Document both, and their rotation.
- [ ] `GH_TOKEN` for the orchestrator: a fine-grained token limited to this repository (issues, pull requests, contents). Document why the scope is needed.
- [ ] Verify `ao agent ls` inside the hub reports the chosen harness as `authorized`.
- [ ] Add the variables to the "Hub" section of `deploy/railway/README.md` and to the RW-4.1.1 threat model (a leaked key is a new asset).

## Acceptance criteria
- [ ] An orchestrator in the hub answers a prompt and can run `gh issue view` against this repository.

## Dependencies
RW-4.1.1. `needs-human`: the operator sets the secrets.
