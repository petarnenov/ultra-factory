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

<!-- status:begin -->
## Status

Status rules: ✅ done requires a closed issue and a merged linked PR; 👀 in review; 🔄 in progress; ⛔ blocked or needs-human; ⬜ todo. ⚠️ marks a closed issue without a merged linked PR; it remains open-status, never done. A merged PR does not close an open issue for status purposes (including docs-only work). A merged umbrella PR can satisfy child tasks when its closing references name them. PRs are linked by closing issue references, closing keywords in the body, or an exact `rw/<task-id>` head branch.

| Task | Issue | Status | PR |
|---|---:|---|---:|
| RW-1.1.1 Task: ADR for trusting a reverse proxy in front of the remote-host listener | [#4](https://github.com/petarnenov/ultra-factory/issues/4) | ✅ done | [#60](https://github.com/petarnenov/ultra-factory/pull/60) |
| RW-1.1.2 Task: ADR for a BFF in front of the daemon orchestrator in the hub | [#5](https://github.com/petarnenov/ultra-factory/issues/5) | ✅ done | [#60](https://github.com/petarnenov/ultra-factory/pull/60) |
| RW-1.1.3 Task: ADR for a dual-stack bind of the remote-host listener | [#6](https://github.com/petarnenov/ultra-factory/issues/6) | ✅ done | [#60](https://github.com/petarnenov/ultra-factory/pull/60) |
| **RW-1.1 Story: Architecture decisions (ADR)** | [#3](https://github.com/petarnenov/ultra-factory/issues/3) | **✅ done** | [#60](https://github.com/petarnenov/ultra-factory/pull/60) |
| RW-1.2.1 Task: Worker Dockerfile | [#8](https://github.com/petarnenov/ultra-factory/issues/8) | ✅ done | [#61](https://github.com/petarnenov/ultra-factory/pull/61) |
| RW-1.2.2 Task: Worker entrypoint | [#9](https://github.com/petarnenov/ultra-factory/issues/9) | ✅ done | [#65](https://github.com/petarnenov/ultra-factory/pull/65) |
| RW-1.2.3 Task: CI build and publish of the worker image | [#10](https://github.com/petarnenov/ultra-factory/issues/10) | ✅ done | [#82](https://github.com/petarnenov/ultra-factory/pull/82) |
| **RW-1.2 Story: Worker image and entrypoint** | [#7](https://github.com/petarnenov/ultra-factory/issues/7) | **✅ done** | — |
| RW-1.3.1 Task: Trusted-proxy mode for lockout | [#12](https://github.com/petarnenov/ultra-factory/issues/12) | ✅ done | [#68](https://github.com/petarnenov/ultra-factory/pull/68) |
| RW-1.3.2 Task: Dual-stack bind of the remote-host listener | [#13](https://github.com/petarnenov/ultra-factory/issues/13) | ✅ done | [#66](https://github.com/petarnenov/ultra-factory/pull/66) |
| RW-1.3.3 Task: Identity probe and Expected-Host-ID guard through a proxy | [#14](https://github.com/petarnenov/ultra-factory/issues/14) | ✅ done | [#71](https://github.com/petarnenov/ultra-factory/pull/71) |
| **RW-1.3 Story: Remote-host listener behind a reverse proxy** | [#11](https://github.com/petarnenov/ultra-factory/issues/11) | **✅ done** | — |
| RW-1.4.1 Task: Deploy to Railway and networking checks | [#16](https://github.com/petarnenov/ultra-factory/issues/16) | ⛔ needs-human | — |
| RW-1.4.2 Task: End-to-end session from the desktop app and runbook | [#17](https://github.com/petarnenov/ultra-factory/issues/17) | ⛔ needs-human | [#83](https://github.com/petarnenov/ultra-factory/pull/83) |
| **RW-1.4 Story: Manual worker deploy and connection from the desktop app** | [#15](https://github.com/petarnenov/ultra-factory/issues/15) | **⛔ needs-human** | — |
| **RW-1 Epic: Worker on Railway, managed from the desktop app** | [#2](https://github.com/petarnenov/ultra-factory/issues/2) | **⛔ needs-human** | — |
| RW-2.1.1 Task: Web `daemon` adapter in `aoBridge` | [#20](https://github.com/petarnenov/ultra-factory/issues/20) | ✅ done | [#62](https://github.com/petarnenov/ultra-factory/pull/62) |
| RW-2.1.2 Task: Base URL without `127.0.0.1` and CSP for the web build | [#21](https://github.com/petarnenov/ultra-factory/issues/21) | ✅ done | [#67](https://github.com/petarnenov/ultra-factory/pull/67) |
| RW-2.1.3 Task: Vite web build target | [#22](https://github.com/petarnenov/ultra-factory/issues/22) | ✅ done | [#64](https://github.com/petarnenov/ultra-factory/pull/64) |
| **RW-2.1 Story: Web build of the renderer with a configurable base URL** | [#19](https://github.com/petarnenov/ultra-factory/issues/19) | **✅ done** | — |
| RW-2.2.1 Task: Capability flag and audit of direct `window.ao` usages | [#24](https://github.com/petarnenov/ultra-factory/issues/24) | ✅ done | [#70](https://github.com/petarnenov/ultra-factory/pull/70) |
| RW-2.2.2 Task: Hiding Electron-only features | [#25](https://github.com/petarnenov/ultra-factory/issues/25) | ✅ done | [#75](https://github.com/petarnenov/ultra-factory/pull/75) |
| **RW-2.2 Story: Hiding Electron-only features in the web build** | [#23](https://github.com/petarnenov/ultra-factory/issues/23) | **✅ done** | — |
| RW-2.3.1 Task: Keyboard shortcuts in the renderer | [#27](https://github.com/petarnenov/ultra-factory/issues/27) | ✅ done | [#74](https://github.com/petarnenov/ultra-factory/pull/74) |
| RW-2.3.2 Task: Web Notifications and badge | [#28](https://github.com/petarnenov/ultra-factory/issues/28) | ✅ done | [#76](https://github.com/petarnenov/ultra-factory/pull/76) |
| RW-2.3.3 Task: Settings in localStorage; clipboard and openExternal | [#29](https://github.com/petarnenov/ultra-factory/issues/29) | ✅ done | [#79](https://github.com/petarnenov/ultra-factory/pull/79) |
| **RW-2.3 Story: Web adapters for shortcuts, notifications, settings, clipboard** | [#26](https://github.com/petarnenov/ultra-factory/issues/26) | **✅ done** | — |
| RW-2.4.1 Task: BFF skeleton with login and session cookie | [#31](https://github.com/petarnenov/ultra-factory/issues/31) | ✅ done | [#73](https://github.com/petarnenov/ultra-factory/pull/73) |
| RW-2.4.2 Task: Reverse proxy for REST, SSE and `/mux` | [#32](https://github.com/petarnenov/ultra-factory/issues/32) | ✅ done | [#80](https://github.com/petarnenov/ultra-factory/pull/80) |
| RW-2.4.3 Task: Host registry and API for adding/removing by address | [#33](https://github.com/petarnenov/ultra-factory/issues/33) | ✅ done | [#84](https://github.com/petarnenov/ultra-factory/pull/84) |
| RW-2.4.4 Task: Renderer multi-host through the BFF instead of Electron `remotes` | [#34](https://github.com/petarnenov/ultra-factory/issues/34) | ✅ done | [#86](https://github.com/petarnenov/ultra-factory/pull/86) |
| **RW-2.4 Story: BFF for the hub (login, proxy, host registry)** | [#30](https://github.com/petarnenov/ultra-factory/issues/30) | **✅ done** | — |
| RW-2.5.1 Task: Hub Dockerfile and supervisor | [#36](https://github.com/petarnenov/ultra-factory/issues/36) | ✅ done | [#91](https://github.com/petarnenov/ultra-factory/pull/91) |
| RW-2.5.2 Task: Hub deployment on Railway and runbook | [#37](https://github.com/petarnenov/ultra-factory/issues/37) | ⛔ needs-human | [#94](https://github.com/petarnenov/ultra-factory/pull/94) |
| **RW-2.5 Story: Hub image and deployment** | [#35](https://github.com/petarnenov/ultra-factory/issues/35) | **⛔ needs-human** | — |
| **RW-2 Epic: Web UI and BFF on Railway** | [#18](https://github.com/petarnenov/ultra-factory/issues/18) | **⛔ needs-human** | — |
| RW-3.1.1 Task: Railway public API spike | [#40](https://github.com/petarnenov/ultra-factory/issues/40) | ✅ done | [#81](https://github.com/petarnenov/ultra-factory/pull/81) |
| RW-3.1.2 Task: `railway-client.ts` | [#41](https://github.com/petarnenov/ultra-factory/issues/41) | ✅ done | [#85](https://github.com/petarnenov/ultra-factory/pull/85) |
| **RW-3.1 Story: Railway API client in the BFF** | [#39](https://github.com/petarnenov/ultra-factory/issues/39) | **✅ done** | — |
| RW-3.2.1 Task: State machine and persisted jobs | [#43](https://github.com/petarnenov/ultra-factory/issues/43) | ✅ done | [#87](https://github.com/petarnenov/ultra-factory/pull/87) |
| RW-3.2.2 Task: Reconcile, orphans and limits | [#44](https://github.com/petarnenov/ultra-factory/issues/44) | ✅ done | [#88](https://github.com/petarnenov/ultra-factory/pull/88) |
| **RW-3.2 Story: Worker lifecycle** | [#42](https://github.com/petarnenov/ultra-factory/issues/42) | **✅ done** | — |
| RW-3.3.1 Task: Worker list | [#46](https://github.com/petarnenov/ultra-factory/issues/46) | ✅ done | [#97](https://github.com/petarnenov/ultra-factory/pull/97) |
| RW-3.3.2 Task: "Add worker" dialog | [#47](https://github.com/petarnenov/ultra-factory/issues/47) | ✅ done | [#102](https://github.com/petarnenov/ultra-factory/pull/102) |
| RW-3.3.3 Task: "Remove worker" dialog | [#48](https://github.com/petarnenov/ultra-factory/issues/48) | ✅ done | [#101](https://github.com/petarnenov/ultra-factory/pull/101) |
| **RW-3.3 Story: "Workers" UI screen** | [#45](https://github.com/petarnenov/ultra-factory/issues/45) | **✅ done** | — |
| **RW-3 Epic: Adding and removing workers from the web UI** | [#38](https://github.com/petarnenov/ultra-factory/issues/38) | **✅ done** | — |
| RW-4.1.1 Task: Threat model and security headers | [#51](https://github.com/petarnenov/ultra-factory/issues/51) | ✅ done | [#98](https://github.com/petarnenov/ultra-factory/pull/98) |
| RW-4.1.2 Task: Railway token scope, rotation, audit log | [#52](https://github.com/petarnenov/ultra-factory/issues/52) | ⬜ todo | — |
| **RW-4.1 Story: Security** | [#50](https://github.com/petarnenov/ultra-factory/issues/50) | **⬜ todo** | — |
| RW-4.2.1 Task: Backup and redeploy strategy | [#54](https://github.com/petarnenov/ultra-factory/issues/54) | ⬜ todo | — |
| RW-4.2.2 Task: Limits and cost monitoring | [#55](https://github.com/petarnenov/ultra-factory/issues/55) | ⬜ todo | — |
| **RW-4.2 Story: Reliability and costs** | [#53](https://github.com/petarnenov/ultra-factory/issues/53) | **⬜ todo** | — |
| RW-4.3.1 Task: Local compose and Playwright e2e | [#57](https://github.com/petarnenov/ultra-factory/issues/57) | ⬜ todo | — |
| RW-4.3.2 Task: Documentation | [#58](https://github.com/petarnenov/ultra-factory/issues/58) | ⬜ todo | — |
| **RW-4.3 Story: E2E test and documentation** | [#56](https://github.com/petarnenov/ultra-factory/issues/56) | **⬜ todo** | — |
| **RW-4 Epic: Hardening, tests, documentation** | [#49](https://github.com/petarnenov/ultra-factory/issues/49) | **⬜ todo** | — |

<!-- status:end -->
