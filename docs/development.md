# Development guide

How to set up, build, run, and test Agent Orchestrator locally.

## Prerequisites

| Tool           | Minimum version | Notes                                                                  |
| -------------- | --------------- | ---------------------------------------------------------------------- |
| Go             | 1.27.1          | `go version` to check; install via [go.dev](https://go.dev/dl/)        |
| Node.js        | 24              | `node --version`; install via [nodejs.org](https://nodejs.org/)        |
| npm            | 10              | Ships with Node.js                                                     |
| Python 3       | -               | `scripts/comment-policy/scan_comments.py`; needed by `make ci-check`   |
| GitHub CLI     | -               | `gh auth login`; needed by the issue-policy check                      |
| Nix (opt.)     | -               | `nix develop` drops you into a shell with all deps; see `../flake.nix` |

Additional runtime dependencies for the daemon:

- **git** (for worktree creation and agent integration)
- **A running agent CLI** (Claude Code, Codex, Aider, etc.). See [the installation
  guide](https://docs.orchestrator.inc/installation).

For source terminal runs on macOS, install/configure tmux; packaged builds
bundle it. New macOS/Linux sessions use detached native PTY hosts, while old
handles and host-start fallback use tmux. `ao doctor` still checks tmux on both.

## Project layout

```text
agent-orchestrator/
  backend/              # Go daemon (Cobra CLI, HTTP API, services, storage)
    cmd/ao/             # CLI entry point
    internal/           # All library code
      cli/              # CLI command implementations
      httpd/            # HTTP controllers, apispec, middleware
      service/          # Business logic layer
      domain/           # Domain types
      ports/            # Port interfaces (contracts)
      storage/          # SQLite migrations, queries, generated code
  frontend/             # Electron + React desktop app
    src/                # Renderer, main, preload
    e2e/                # Playwright end-to-end tests
  packages/
    mobile/             # React Native (Expo) mobile companion app
    ao/                 # Legacy npm CLI package (frozen)
  docs/                 # Architecture, ADRs, CLI docs, status
  CONTRIBUTING.md       # Contribution guide
```

## Getting the code

```bash
git clone https://github.com/Untrivial-ai/agent-orchestrator.git
cd agent-orchestrator
make setup    # checks prerequisites, installs npm + Go deps, runs doctor
```

The root `Makefile` wraps every common command. `make help` lists them all:

```bash
make doctor          # check toolchain versions and installed dependencies
make dev             # Electron desktop app in dev mode
make dev-web         # renderer only, in a browser
make daemon          # run the daemon from source
make ao ARGS=status  # run the ao CLI from source
make test            # backend + frontend unit tests
make check           # lint, vet, typecheck, frontend tests
make ci-check        # comment policy, issue policy, CI lint job
make api / make sqlc # regenerate OpenAPI types / sqlc code
```

On a shared machine, run one heavy check at a time: do not start a second
compiler, linter, or test suite from another shell while one is already running.
The local entry points cap parallelism so a single run does not take every core.
`make test-backend`, `make test-race`, and `npm run lint` pass
`-p ${GO_TEST_PARALLELISM:-2}` to `go test`; `make test-frontend` and
`npm --prefix frontend run test` cap Vitest with `AO_VITEST_MAX_FORKS` (default
`2`, `0` leaves Vitest's own limit). `make check-all` and `make check` run their
heavy steps serially, including under `make -j`.

### Branching

```bash
git checkout -b ao/my-feature-branch origin/dev
```

Keep your branch up to date by rebasing on the integration branch:

```bash
git fetch origin
git rebase origin/dev
```

### Committing

Keep commits atomic. Use one logical change per commit. Stage related changes and
commit with a conventional message:

```bash
git add <files>
```

Commit message tags:

| Tag        | When to use                           |
| ---------- | ------------------------------------- |
| `feat`     | New feature                           |
| `fix`      | Bug fix                               |
| `docs`     | Documentation only                    |
| `test`     | Adding or fixing tests                |
| `refactor` | Code change with no functional change |
| `chore`    | Maintenance, tooling, dependencies    |

Use **trailers** to provide additional context:

```bash
git commit -m "fix: handle nil pointer in session lookup

The session resolver panicked when the store returned a nil session
without an error. Return ErrNotFound instead.

Signed-off-by: Your Name <your.email@example.com>
Co-authored-by: Contributor Name <contributor@example.com>"
```

## Backend

### Build

```bash
cd backend
go build ./...
```

### Run the daemon

```bash
cd backend
# Start the daemon (loopback HTTP server on 127.0.0.1)
go run ./cmd/ao daemon
```

The CLI is built with Cobra. From `backend/`, run `go run ./cmd/ao --help` for
available commands.

### Run tests

```bash
cd backend
go test -p 2 ./...              # all tests
go test -race -p 2 ./...        # with race detection
go test -v ./internal/cli/      # a specific package
```

### Lint

```bash
npm run lint
```

`npm run lint` runs the backend suite and golangci-lint with `GOTOOLCHAIN`
forced to `backend/go.mod`'s version, as CI's `setup-go` does. It also clears
`AO_*` for the backend suite, because CI runs without them and an AO worker
session would otherwise inject its own session state into tests that read it.
To run only the `go.yml` lint job, use `bash scripts/lint.sh --lint-only`.

### Checks that mirror CI

The comment policy, issue tracking policy, and lint job each use inputs that a
bare command does not supply. `make ci-check` supplies them:

```bash
make ci-check                              # all three
make ci-check ARGS="--only comment,lint"   # before the pull request exists
make ci-check ARGS="--only issue --pr 412" # a specific pull request
```

- **Comment policy** (`.github/workflows/comment-policy.yml`): compares against
  the pull request's base commit, or `origin/dev` when there is no pull request
  yet, instead of the raw command's `origin/main` default. It also forces the
  Go toolchain and runs the same test step as the workflow. On a `main` push,
  pass `--base <before-sha>` (or `--base origin/main` before pushing). It needs
  `frontend/node_modules`; install it with
  `npm ci --prefix frontend --ignore-scripts` (the workflow's command).
- **Issue policy** (`.github/workflows/issue-policy.yml`): sets `GH_TOKEN`,
  `GITHUB_REPOSITORY`, and `PR_NUMBER` from your environment, `origin`, and the
  current branch's pull request. It reads the live title, body, and issue labels,
  so it needs an open pull request and GitHub API access. Before the pull
  request exists, the issue check is skipped; run `make ci-check` again after
  opening it. The workflow runs on the same inputs.
- **Lint** (`go.yml` job `lint`): runs golangci-lint v2.14.0 in `backend` with
  `GOTOOLCHAIN` pinned to `backend/go.mod`, matching `setup-go` and the pinned
  action version.

`make check` and `make check-all` also run the comment policy, and the issue
policy when an open pull request exists. They keep their broader local suite;
`make ci-check` is the focused entry point for the three CI policy jobs.

Checks that cannot run locally: the issue policy needs an open pull request and
GitHub API access; Docker jobs (`cli-e2e`, `railway-e2e`, and the image builds)
need Docker (`make ci-local` runs the workflows through agent-ci); the macOS and
Windows jobs need those runners; and the release/publish workflows are not
validation steps.

### Code generation

```bash
# Regenerate sqlc code after editing queries or schema
npm run sqlc

# Regenerate OpenAPI spec and frontend TypeScript types
npm run api
```

## Frontend

### Install dependencies

```bash
cd frontend
npm install
```

### Run in development mode

```bash
cd frontend
npm run dev            # Electron dev mode
npm run dev:web        # Web-only (no Electron, for quick UI iteration)
```

### Build

```bash
cd frontend
npm run package        # Package for current platform
npm run make           # Create distributables when platform packaging deps are installed
```

On a fresh Linux machine, treat `npm run package` as the default local build
path. `npm run make` also needs Linux packaging tools that are not provided by a
minimal setup or by `nix develop` today:

- `rpm` / `rpmbuild` for the RPM target
- the usual distro packaging toolchain required by Electron Forge makers

CI installs `rpm` explicitly before running `npm run make`. Do the same locally
if you need Linux distributables, or skip `npm run make` on a fresh setup.

### Run tests

```bash
cd frontend
npm run test           # Vitest unit tests in a simulated renderer environment
npm run test:e2e       # Playwright browser-based renderer E2E tests
npx playwright show-report  # View Playwright report
```

`npm run test` caps Vitest at two forks by default. Set `AO_VITEST_MAX_FORKS` to
raise it on a larger machine, or to `0` to leave Vitest's own fork limit.

### Typecheck

```bash
cd frontend
npm run typecheck
```

Or from repo root:

```bash
npm run frontend:typecheck
```

## Mobile companion app

The mobile companion app is still being wired into the contributor docs. Do not
assume `packages/mobile/README.md` is a complete setup guide on this branch.
Until a tracked guide lands, use the desktop/backend workflow above and check
open issues/PRs for current mobile-specific setup notes.

## Running end-to-end

1. Start the desktop app with `npm run dev` from `frontend/`.
2. The Electron main process starts and supervises the loopback daemon for you.
3. Use `npm run dev:web` only for renderer-only development; it does not launch Electron.

For CLI-only usage, open two terminals:

**Terminal 1: start the daemon.**

```bash
cd backend
go run ./cmd/ao daemon
```

**Terminal 2: interact while the daemon is running.**

```bash
cd backend
go run ./cmd/ao status
go run ./cmd/ao --help
```

## Testing tips

### Backend

- Backend tests use `httptest.Server` and injected fakes. They do not require a
  real daemon.
- Run the narrowest relevant test suite first (e.g. `go test ./internal/cli/`),
  then the full suite.

### Frontend

- Unit tests use Vitest and run in a simulated renderer environment.
- E2E tests use Playwright against the web renderer started by `npm run dev:web`;
  they do not launch the full Electron app.
- After changing API types, run `npm run api` from root to regenerate
  `frontend/src/api/schema.ts`.

## Troubleshooting

### Backend build / test failures

| Symptom                              | Likely cause                               | Fix                                                                                                                                                                                                                                                                               |
| ------------------------------------ | ------------------------------------------ | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `go: go.mod requires go >= 1.27`     | Wrong Go version                           | `go version`; install Go 1.27.1+ from [go.dev](https://go.dev/dl/)                                                                                                                                                                                                                                    |
| `sqlc generate` produces errors      | Query SQL syntax or schema migration issue | Check `backend/internal/storage/sqlite/queries/` for SQL syntax, placeholder counts, and referenced columns/tables; if you changed the schema, add a new migration in `backend/internal/storage/sqlite/migrations/` instead of editing an existing one, then rerun `npm run sqlc` |
| `openapi.yaml` is stale              | Changed DTOs without regenerating          | Run `npm run api` from repo root                                                                                                                                                                                                                                                  |
| `golangci-lint` failures             | Linter version mismatch                    | Install v2.14.0 or use `npm run lint` from root                                                                                                                                                                                                                                   |
| Tests fail with "connection refused" | Test tries real daemon                     | Tests should use `httptest`; check for `go test ./...` without a live daemon                                                                                                                                                                                                      |

### Frontend build / test failures

| Symptom                               | Likely cause            | Fix                                                          |
| ------------------------------------- | ----------------------- | ------------------------------------------------------------ |
| `npm run typecheck` has type errors   | API types out of sync   | Run `npm run api` from repo root to regenerate               |
| `npm run dev` fails on native modules | Missing build tools     | Install Python + C++ build tools for `node-gyp`              |
| `npm install` or `npm ci` fails       | Node.js version too old | `node --version`; must be 24+ (see prerequisites above) |
| Blank window or crash on Linux        | Broken GPU driver stack | Start with `AO_DISABLE_GPU=1` to skip hardware acceleration  |

### Code generation drift

If CI fails on `sqlc-drift`, run the pinned generator from the repository root
and commit the generated storage changes together with their SQL/config source:

```bash
npm run sqlc
git diff --exit-code -- backend/internal/storage/sqlite/gen
```

The diff check should pass after committing the regenerated files. Do not edit
`gen/` by hand or use a different sqlc version; the version in `package.json` is
authoritative. Review any unexpected query or generated type changes before
committing. The storage tests also check that the config is valid YAML and keeps
the boolean overrides for `agent_switch_failure_policy.enabled` and
`app_settings.cloud_offering`.

If CI fails on the `api-drift` check, the OpenAPI-generated files are out of sync with source. Regenerate them locally and commit the updated files:

```bash
npm run api
```

If regeneration introduces unexpected diffs beyond your changes, check that your local tool versions match CI (Go 1.27.1+, Node 24, npm 10+).

## OpenAPI spec and generated types

The API is defined in Go controller DTOs and operation registrations. Edit
these source files, then regenerate:

```bash
npm run api
```

The generated artifacts are:

- `backend/internal/httpd/apispec/openapi.yaml`
- `frontend/src/api/schema.ts`

Both must be committed together with the Go changes. CI verifies they are
in sync.
