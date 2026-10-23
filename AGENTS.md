# AGENTS.md

Operational guidance for coding agents working in this repository. Keep changes small, match the current rewrite architecture, and prefer the documented daemon/API boundaries over behavior from the old TypeScript implementation.

This file holds what every task needs. Load the page for your task when it applies:

| When you are about to... | Read |
| --- | --- |
| Run builds, tests, CI checks, the hub BFF checks, or the desktop lab | [`docs/agents/commands.md`](docs/agents/commands.md) |
| Change a daemon HTTP request or response shape | [`docs/agents/api-contract.md`](docs/agents/api-contract.md) |
| Branch, merge, or promote `dev` to `main` | [`docs/agents/git-flow.md`](docs/agents/git-flow.md) |
| Touch a daemon listener, route auth, remote access, or the Railway hub | [`docs/agents/network-boundaries.md`](docs/agents/network-boundaries.md) |
| Spawn workers, run reviews, or pick a machine | [`docs/agents/orchestration.md`](docs/agents/orchestration.md) |
| Change install paths, release workflows, or macOS artifacts | [`docs/agents/distribution.md`](docs/agents/distribution.md) |
| Use or upgrade the OpenSpec authoring workflow | [`docs/agents/openspec.md`](docs/agents/openspec.md) |
| File an issue or check its labels | [`docs/issue-policy.md`](docs/issue-policy.md) |
| Write a PR description | [`.agents/skills/pr-description/SKILL.md`](.agents/skills/pr-description/SKILL.md) |
| Resolve a complex architecture decision through the consul workflow | [`.agents/skills/geo.architect/SKILL.md`](.agents/skills/geo.architect/SKILL.md) |
| Make a visual or UI decision | [`DESIGN.md`](DESIGN.md) |

## Language

**All documents, comments, and similar text in this repository are written in English only.** That covers:

- Markdown/MDX documentation, ADRs, backlog items, runbooks, and READMEs;
- code comments in every language (Go, TypeScript, shell, SQL, YAML, and so on), including doc comments;
- commit messages, PR titles and descriptions, issues, and issue and review comments.

This applies even when you talk to the user in another language: conversation can follow the user, the repository text cannot. The only exceptions are product strings that go through i18n and the existing `translations/` READMEs. Do not add text in another language; when you edit text that is not in English, translate it.

## Repo layout

- `backend/`: Go rewrite of Agent Orchestrator with the Cobra `ao` CLI, loopback HTTP daemon, services, SQLite storage, lifecycle/reaper, runtime/workspace/agent/tracker adapters, terminal mux, and tests.
- `frontend/`: Electron + React supervisor wired to the daemon through the generated typed client. Treat it as a thin supervisor/UI surface; do not move daemon logic into it.
- `docs/`: Current architecture and status notes. Start here before changing lifecycle, CLI, agents, storage, or daemon behavior.
- `test/`: External smoke/e2e assets, including the CLI fresh-install container check.
- `.github/workflows/`: CI definitions. Mirror these commands locally when possible.

Where to look first: `README.md` (quickstart), `docs/README.md` (docs index), `docs/architecture.md` (backend mental model and load-bearing rules), `docs/STATUS.md` (what is shipped), `docs/cli/README.md` (CLI shape), and `docs/documentation-map.md` (which docs are contract). Code entry points:

- CLI commands: `backend/internal/cli/*.go`; follow nearby command/test patterns before adding a new style.
- HTTP controllers and DTOs: `backend/internal/httpd/controllers/`.
- Service read/write boundaries: `backend/internal/service/`.
- Domain vocabulary: `backend/internal/domain/`.
- Port contracts: `backend/internal/ports/`.
- SQLite queries/migrations/store: `backend/internal/storage/sqlite/`.
- Generated sqlc code: `backend/internal/storage/sqlite/gen/`.

## Core commands

From the repo root. The full catalog, parallelism settings, and the desktop lab are in [`docs/agents/commands.md`](docs/agents/commands.md).

```bash
make ci-check                        # CI comment policy, issue policy, and lint job
npm run lint                         # backend go test -p 2 + golangci-lint v2.14.0
npm run frontend:typecheck           # frontend TypeScript check
npm run sqlc                         # regenerate backend/internal/storage/sqlite/gen from queries/schema
npm run api                          # regenerate OpenAPI spec + frontend TS types
```

**Local validation on a shared machine runs one heavy check at a time.** Do not start a second compiler, linter, or test suite from another shell while one is running.

When showing or demoing frontend changes, run `ao preview [url]` from inside the session so the change renders in the desktop browser panel (the inspector rail's Browser tab); do not just describe it.

## Coding conventions

- Keep each code comment to at most two physical lines and 80 characters per line, including indentation and delimiters. Adjacent `//` lines form one comment. Markdown prose is not subject to this limit.
- The automated check tokenizes Go and TypeScript/TSX/JavaScript with Go's standard parser and the existing TypeScript compiler. Generated files, Go build/compiler directives, TypeScript tooling directives, narrow formatter/linter directives, and SPDX/copyright notices are preserved and narrowly excluded. Other handwritten formats are not yet covered by automated enforcement; the rule still applies to their comments.
- The committed `scripts/comment-policy/baseline.json` records existing Go and TypeScript-family violations per file. It prevents the recorded debt from growing; it does not mean existing violations are compliant or remediated. Run `make ci-check ARGS="--only comment"` when changing comments; it uses the pull request's base and the pinned toolchain. `npm run comment:check` and `npm run comment:test` remain the raw checks.
- Keep every change surgical and directly tied to the task. Avoid drive-by cleanup, broad renames, formatting churn, speculative abstractions, and architectural refactors unless the task explicitly asks for them.
- Follow existing Go package boundaries. CLI code should call daemon HTTP routes through shared CLI client helpers; it should not open SQLite, spawn runtimes, or call adapters directly.
- Keep Cobra commands in the relevant command file and table-test them in the style of `backend/internal/cli/*_test.go`.
- Mirror existing response/request DTOs in the CLI instead of importing HTTP controller packages into CLI code, unless the package already establishes that dependency.
- Return usage errors as `usageError` so CLI misuse exits 2; runtime/daemon failures should exit 1.
- Preserve API error envelopes and request IDs when surfacing daemon errors.
- Use `context.Context` as the first argument for functions that do I/O or blocking work.
- Do not add abstractions for one-off use cases. Add helpers only when they remove duplication across real call sites.
- Tests should cover the user-visible behavior and boundary being changed: happy path, validation/missing args, daemon error envelopes, and any destructive confirmation path.
- Test coverage must be at least 75% for each Go package the change touches, measured with `go test -cover ./<pkg>/...`. It is the owner's bar for new and changed code, not a whole-repo figure. CI does not enforce it, so report the measured numbers in the PR body and explain any package that stays below. The frontend has no coverage tooling yet (`@vitest/coverage-v8` is not a dependency), so frontend changes report coverage as not measured.

## Hard rules and boundaries

- The daemon's **primary (loopback) listener** stays bound to `127.0.0.1` and unauthenticated. Do not change its bind host or add auth to it.
- The only other network-facing bind is the **opt-in remote-host listener** (Connect Mobile, Tailscale, Railway hub), always behind `authMiddleware` or the Tailscale identity check. Exactly one route is exempt: `GET /api/v1/identity`; any further unauthenticated route needs its own ADR. Do not add any other network-facing bind. The full contract and ADRs 0001, 0003, 0005-0008 and 0011 are in [`docs/agents/network-boundaries.md`](docs/agents/network-boundaries.md).
- The CLI is a thin client. Do not port old in-process TypeScript CLI behavior that bypasses daemon HTTP routes.
- Do not store derived/display session status. Status is derived from durable facts (`activity_state`, `is_terminated`, PR/check/comment facts) at service read time.
- Do not treat failed/unknown runtime probes as proof a session is dead.
- Do not force-delete dirty registered worktrees.
- Do not modify already-merged SQLite migrations. Add a new migration instead.
- Do not hand-edit `backend/internal/storage/sqlite/gen/*`; change `backend/internal/storage/sqlite/queries/*` or migrations and run `npm run sqlc`.
- SQLite change events come from DB triggers into `change_log`; do not add parallel manual CDC emission from store methods unless the architecture changes explicitly.
- Keep generated OpenAPI/API DTO drift in mind: controller response shapes live in `backend/internal/httpd/controllers/dto.go` and tests may assert CLI/HTTP wire compatibility.
- Do not add network calls to tests unless the package already has an integration/e2e pattern for them. Prefer `httptest`, fakes, and injected dependencies.
- Do not commit local run state, daemon data, temporary worktrees, build outputs, or credentials.
- All AO-owned app state lives under `~/.ao` only. The daemon's data dir, `running.json`, worktrees, and the Electron supervisor's `userData` (Chromium cache, cookies, local/session storage, crash dumps) must resolve under `~/.ao` (overridable via `AO_DATA_DIR`/`AO_RUN_FILE`). Never write to or otherwise use `~/Library/Application Support` or any other OS default app-data location for AO state. The sole read exception is an explicit, user-initiated browser-profile import: it may read only validated, known source-browser profile files, must never write to the source profile, must keep source paths out of the renderer, and must place every snapshot, staging file, and imported result under `~/.ao`. `main.ts` pins Electron's `userData` to `~/.ao/electron`; do not remove that override or rely on Electron's default path.

## Issues and pull requests

- Every change is tracked by a GitHub issue with exactly one type label and one priority label; see [`docs/issue-policy.md`](docs/issue-policy.md). Backlog labels (`rw*`, `co*`, `status:*`, `wave:*`, `host:*`, `needs-human`) do not satisfy the requirement.
- A pull request must reference its issue with `Closes #NNN`; `.github/workflows/issue-policy.yml` fails it otherwise. Only the `dev` into `main` promotion and the agent-pin refresh on `chore/agent-cli-pins` are exempt.
- Branch from `dev` in a task worktree and target the PR at `dev`, unless explicitly continuing an existing PR; only a `dev` promotion merges into `main`. Never work in the invoking working tree. Details: [`docs/agents/git-flow.md`](docs/agents/git-flow.md).
- When creating or updating a PR, follow `.agents/skills/pr-description/SKILL.md` so the published description starts with verified category change counts.
- Before creating or handing over a PR, run all CI validation jobs locally using the workflow commands, pinned runtimes, and CI environment (including complete suites, not only focused tests). Run `make ci-check` for the comment policy, issue policy, and lint job; it supplies their CI inputs. Fix failures and rerun the affected full suites before sharing the PR. A local pass is not a guarantee: verify the remote checks too. If a job cannot run locally (for example, an unavailable native OS runner, Docker, or required credentials), explicitly report the exact gap and verify that job in CI; never label it locally passed. Do not execute publishing or production deployment as a validation step.
- Keep one issue per PR. If asked for separate work, create a separate branch and PR.
- Use conventional commit messages (`feat:`, `fix:`, `docs:`, `test:`, `chore:`).
- Explain intentional omissions in the PR body, especially when the TypeScript original had more behavior than the Go rewrite domain currently supports.
- Run the narrowest relevant tests first, then the repo/CI commands that match the touched area.
