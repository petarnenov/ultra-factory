# Commands

Loaded on demand from [`AGENTS.md`](../../AGENTS.md). Read it before running builds, tests, or CI checks.

From the repo root unless noted. The root `Makefile` wraps these (`make help`, `make setup`, `make doctor`, `make check`, `make check-all`, `make ci-check`):

```bash
make ci-check                        # CI comment policy, issue policy, and lint job
npm run lint                         # backend go test -p 2 + golangci-lint v2.14.0
npm run frontend:typecheck           # frontend TypeScript check
npm run sqlc                         # regenerate backend/internal/storage/sqlite/gen from queries/schema
npm run api                          # regenerate OpenAPI spec + frontend TS types (see API contract changes below)
npx @redwoodjs/agent-ci run --all    # local workflow validation; requires Docker socket
```

`make ci-check` is the documented way to reproduce the policy jobs. It resolves
the pull request's comparison base, forces `GOTOOLCHAIN` to `backend/go.mod`'s
version, and supplies the environment the policy scripts read. The raw
`npm run comment:check` defaults to `origin/main`, which is not the base of a
`dev`-targeted pull request.

**Local validation on a shared machine runs one heavy check at a time.** Do not
start a second compiler, linter, or test suite from another shell while one is
running. The local entry points cap parallelism; raise it deliberately only on a
larger machine:

```bash
make test-backend                    # go test -p ${GO_TEST_PARALLELISM:-2} ./...
make test-race                       # go test -race -p ${GO_TEST_PARALLELISM:-2} ./...
make test-frontend                   # Vitest with AO_VITEST_MAX_FORKS forks (default 2)
npm run lint                         # go test -p ${GO_TEST_PARALLELISM:-2} then golangci-lint
```

`AO_VITEST_MAX_FORKS=0` leaves Vitest's own fork limit. CI keeps its own
invocations and parallelism. `make check-all` and `make check` run their heavy
steps serially, including under `make -j`.

Backend-specific checks:

```bash
cd backend
go build ./...
go test -p 2 ./...
go test -race -p 2 ./...
go vet ./...
go run ./cmd/ao start
```

Frontend-specific checks:

```bash
cd frontend
npm run typecheck
npm run build
npm run build:web       # standalone web renderer → dist-web/ (VITE_AO_WEB=1; dev:web:real runs it against a real daemon via AO_DEV_API_TARGET)
```

Railway hub (web BFF, `packages/hub-bff`) checks, from the repo root:

```bash
npm run hub-bff:check                       # BFF typecheck + vitest (same as make hub-bff-check)
npm --prefix packages/hub-bff test          # BFF tests only
./deploy/railway/compose.e2e.sh             # local hub + worker Compose and Playwright web e2e; requires Docker
```

When showing or demoing frontend changes, run `ao preview [url]` from inside the session so the change renders in the desktop browser panel (the inspector rail's Browser tab); do not just describe it.

## Desktop lab (running the real Electron app for UI review)

For visual verification that needs the real app rather than `ao preview` or `dev:web`, follow the `ao-desktop-dev` skill (`.agents/skills/ao-desktop-dev/SKILL.md`); it owns launch, data modes, restart, and safe shutdown. The short version:

- **Run the branch in its own task worktree**, never in the invoking working tree (it may hold unrelated uncommitted work):
  ```bash
  git worktree add ../uf-worktrees/<slug> <branch>   # the PR branch under review
  cd ../uf-worktrees/<slug>/frontend
  npm ci                                             # never symlink node_modules from another checkout (see below)
  ```
- **Use isolated mode by default.** `env -u AO_DATA_DIR -u AO_RUN_FILE -u AO_PORT npm run dev` keeps the dev daemon on port `3002` with `~/.ao/dev/running.json`, `~/.ao/dev/data`, and `~/.ao/dev/electron`. All lab state stays under `~/.ao`; never point a lab at `/tmp` or another OS location. Use real-data mode only when the user asks to see this machine's real projects, as the skill describes.
- **Run two worktrees at once** only with separate dev state for each: `AO_DEV_ELECTRON_DIR`, `AO_RUN_FILE`, and `AO_DATA_DIR` under `~/.ao/dev/<name>/`. Otherwise the second instance loses the Electron profile lock and exits silently.
- **Run a real `npm ci`.** Symlinking another checkout's `node_modules` makes React resolve from two physical paths and shares the Vite optimizer cache across checkouts: the symptom is a black window with `Invalid hook call` / `useSyncExternalStore` errors in the renderer console. It also writes `.vite/deps` output into the other checkout's tree.
- **Diagnose with `ELECTRON_ENABLE_LOGGING=1`.** Renderer console errors (the cause of most blank windows) land in the launcher's stdout.
- **Killing the forge wrapper is not enough.** The Electron main process cmdline is just `Electron .`, so `pkill -f electron-forge` leaves the app running and the next launch exits silently. Stop the verified `Electron.app/.../MacOS/Electron` main process of this checkout before relaunching; never kill every Electron process.
- A missing-dependency warning for a lazily imported module (for example `mermaid`, which is dynamically imported on first use) does not blank the window; treat it as informational.
