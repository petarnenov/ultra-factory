# Backlog: AO on Railway (key `RW`)

Source: `../railway-report.md`. Option A: hub (BFF + daemon orchestrator) + daemon workers as Railway services.

## Structure
```
backlog/
  RW-1-rabotnik-v-railway/        epic   → EPIC.md
    RW-1.1-adr/                   story  → STORY.md
      RW-1.1.1-adr-bff.md         task   (subtasks are a checklist in the task)
```
- **Epic** = a phase from the report. **Story** = a goal visible to a user or operator. **Task** = one PR-sized unit of work. **Subtask** = a checkbox in the task.
- Every task has: Goal, Context (files), Subtasks, Acceptance criteria, Verification, Estimate, Dependencies.
- Estimates are in person-days (pd = person-days) for a developer who knows the repo.

## Execution order
| Epic | Content | pd | Stage |
|---|---|---|---|
| RW-1 | Worker on Railway, managed from the desktop app | 6–9 | MVP-1 |
| RW-2 | Web UI + BFF on Railway | 11–18 | MVP-2 |
| RW-3 | Adding and removing workers from the UI | 5–8 | Full goal |
| RW-4 | Hardening, tests, documentation | 3–5 | Full goal |

Critical path: RW-1.1 → RW-1.2 → RW-1.3 → RW-1.4 (validates Railway) → RW-2.4 (BFF) → RW-3.

## Repo rules every task follows
- `AGENTS.md` hard rules: the loopback listener stays on `127.0.0.1` without auth. A new network bind or an unauthenticated route requires an ADR (`docs/adr/`).
- All app state lives under `~/.ao` (`AO_DATA_DIR`). In a container this is the volume.
- The CLI is a thin client over the daemon HTTP API.
- API changes: `npm run api` regenerates the OpenAPI spec and the TS types.
- Before a PR: `make check`.
