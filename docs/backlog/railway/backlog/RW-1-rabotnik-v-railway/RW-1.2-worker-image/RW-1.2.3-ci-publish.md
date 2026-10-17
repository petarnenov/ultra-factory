# RW-1.2.3 · Task: CI build and publish of the worker image
**Status:** ✅ done (#82)

**Type:** CI. **Estimate:** 0.5–1 pd. **Story:** RW-1.2.

## Goal
A GitHub Actions workflow that builds `deploy/railway/worker/Dockerfile` for amd64/arm64 and publishes it to GHCR on a tag and on changes to the relevant paths.

## Context
- `.github/workflows/` contains the current CI definitions; `AGENTS.md` requires a local mirror with `npx @redwoodjs/agent-ci run --all` (Docker).
- `AGENTS.md` "Distribution": only the release conductor publishes desktop artifacts. The worker image is a new channel: tag it with `worker-v<semver>` and `sha-<short>`, not with the desktop versions, so they do not get mixed up.

## Subtasks
- [ ] `.github/workflows/worker-image.yml`: triggers `push` on `main` for paths `backend/**`, `deploy/railway/worker/**`, `frontend/scripts/build-acp-runtime.mjs`; `push` of tags `worker-v*`; `workflow_dispatch`.
- [ ] `docker/setup-qemu-action`, `setup-buildx-action`, `login-action` to `ghcr.io` with `GITHUB_TOKEN`, `build-push-action` with cache `type=gha`.
- [ ] Smoke job: run the image with a temporary volume, check `/api/v1/identity` on :3011 → 200.
- [ ] Record the image URL and the tagging scheme in `deploy/railway/worker/README.md`.
- [ ] Add the workflow to the agent-ci local check, if applicable.

## Acceptance criteria
- [ ] `ghcr.io/<org>/ao-worker:sha-<x>` and `:worker-v0.1.0` exist and the smoke job is green.
