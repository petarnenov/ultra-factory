# Backlog: Hierarchical coordinators (key `CO`)

Source: `../coordinator-report.md`.

## Structure

```text
backlog/
  CO-1-coordinator-contract/       epic -> EPIC.md
    CO-1.1-measurement/             story -> STORY.md
      CO-1.1.1-resource-baseline.md task
```

- **Epic** = a delivery phase. **Story** = an operator-visible capability.
  **Task** = one reviewable implementation unit. Subtasks are checklists.
- Every task includes Goal, Context, Subtasks, Acceptance criteria,
  Verification, Estimate, and Dependencies.
- Estimates are person-days for a developer familiar with the repository.
- Status icons follow the [RW backlog legend](../../railway/backlog/README.md#status):
  ✅ done, 👀 in review, 🔄 in progress, ⛔ blocked or needs-human, ⬜ todo.

## Execution order

| Epic | Content | Estimate | Stage |
|---|---|---:|---|
| CO-1 | Measure load and define coordinator ownership and routing | 2–3 pd | Design gate |
| CO-2 | Persist ownership and route worker reports to coordinators | 4–6 pd | Core |
| CO-3 | Spawn and operate two bounded coordinators | 3–5 pd | Pilot |
| CO-4 | Recovery, end-to-end validation, and rollout | 2–4 pd | Release |

Critical path: CO-1.1 → CO-1.2 → CO-2.1 → CO-2.2 → CO-3.1 → CO-3.2 → CO-4.

## Invariants

- One root orchestrator per project remains the system of record.
- Initial limits are two coordinators, three active workers per coordinator,
  and six workers total. Reject over-capacity requests with a clear error.
- Every worker has one durable coordinator owner. Ownership changes are
  explicit and auditable.
- Reports are delivered to the worker's owner; summaries and escalations go to
  the root. Delivery remains retry-safe and idempotent.
- Only the root may perform final integration and merge.
- Existing direct root-to-worker workflows remain supported.
- All state is stored through the daemon's existing persistence boundaries;
  the CLI remains a thin daemon client.
- Follow the API contract and migration rules in `AGENTS.md`.

## GitHub issue tracking

GitHub issues mirror the Epic → Story → Task hierarchy. Issues are nested as
sub-issues; labels `co`, `co:epic`, `co:story`, and `co:task` identify the
initiative and item type. The GitHub Project tracks all 24 issues with Status, Stage, estimate, parent
issue, and sub-issue progress fields: [AO: Hierarchical Coordinators (CO)](https://github.com/users/petarnenov/projects/2).

| Item | GitHub issue | Status |
|---|---:|---|
| [CO-1] Epic: Measure load and define coordinator ownership | [#205](https://github.com/petarnenov/ultra-factory/issues/205) | In progress |
| [CO-1.1] Story: Baseline resource use under six workers | [#209](https://github.com/petarnenov/ultra-factory/issues/209) | In progress |
| [CO-1.1.1] Task: Record per-process resource baseline | [#218](https://github.com/petarnenov/ultra-factory/issues/218) | Needs human |
| [CO-1.2] Story: Coordinator ownership contract | [#210](https://github.com/petarnenov/ultra-factory/issues/210) | Done |
| [CO-1.2.1] Task: ADR for coordinator hierarchy and report routing | [#219](https://github.com/petarnenov/ultra-factory/issues/219) | Done |
| [CO-2] Epic: Persist coordinator ownership and route reports | [#206](https://github.com/petarnenov/ultra-factory/issues/206) | Done |
| [CO-2.1] Story: Durable coordinator and worker ownership | [#211](https://github.com/petarnenov/ultra-factory/issues/211) | Done |
| [CO-2.1.1] Task: Persist coordinator ownership | [#220](https://github.com/petarnenov/ultra-factory/issues/220) | Done |
| [CO-2.1.2] Task: Expose ownership through daemon and CLI APIs | [#221](https://github.com/petarnenov/ultra-factory/issues/221) | Done |
| [CO-2.2] Story: Owner-routed reports and root summaries | [#212](https://github.com/petarnenov/ultra-factory/issues/212) | Done |
| [CO-2.2.1] Task: Route local and remote reports to worker owner | [#222](https://github.com/petarnenov/ultra-factory/issues/222) | Done |
| [CO-2.2.2] Task: Deliver coordinator summaries and escalations to root | [#223](https://github.com/petarnenov/ultra-factory/issues/223) | Done |
| [CO-3] Epic: Run two bounded coordinators | [#207](https://github.com/petarnenov/ultra-factory/issues/207) | Done |
| [CO-3.1] Story: Spawn coordinators and enforce capacity | [#213](https://github.com/petarnenov/ultra-factory/issues/213) | Done |
| [CO-3.1.1] Task: Add coordinator spawn and capacity rules | [#224](https://github.com/petarnenov/ultra-factory/issues/224) | Done |
| [CO-3.2] Story: Coordinate worker groups and return summaries | [#214](https://github.com/petarnenov/ultra-factory/issues/214) | Done |
| [CO-3.2.1] Task: Define coordinator operating prompt and handoff | [#225](https://github.com/petarnenov/ultra-factory/issues/225) | Done |
| [CO-3.3] Story: Inspect coordinator hierarchy | [#215](https://github.com/petarnenov/ultra-factory/issues/215) | Done |
| [CO-3.3.1] Task: Show coordinator ownership in session listings | [#226](https://github.com/petarnenov/ultra-factory/issues/226) | Done |
| [CO-4] Epic: Validate and roll out hierarchical coordination | [#208](https://github.com/petarnenov/ultra-factory/issues/208) | In progress |
| [CO-4.1] Story: End-to-end pilot and resource comparison | [#216](https://github.com/petarnenov/ultra-factory/issues/216) | In progress |
| [CO-4.1.1] Task: Run coordinator hierarchy end-to-end | [#227](https://github.com/petarnenov/ultra-factory/issues/227) | Needs human |
| [CO-4.2] Story: Roll out and recover the coordinator hierarchy | [#217](https://github.com/petarnenov/ultra-factory/issues/217) | Done |
| [CO-4.2.1] Task: Document coordinator rollout and rollback | [#228](https://github.com/petarnenov/ultra-factory/issues/228) | Done |

## Repo rules every task follows

- Keep daemon behavior in backend services and controllers, not in the BFF.
- Add SQLite changes with new migrations and source queries; regenerate sqlc.
- API changes must regenerate OpenAPI and frontend TypeScript types with
  `npm run api`.
- Keep repository documents and comments in English.
- Do not deploy or change live Railway configuration as part of local task
  validation.
