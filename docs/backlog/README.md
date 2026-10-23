# Backlog

Planning documents that are not yet implementation: analysis reports and the
epic → story → task → subtask breakdowns derived from them. Each subdirectory
is one initiative. Items here describe intended work; the code and `STATUS.md`
remain the source of truth for what is shipped.

| Initiative | Report | Backlog |
| --- | --- | --- |
| OpenSpec specification driven development across the SDLC | [openspec/blueprint.md](openspec/blueprint.md) | [openspec/README.md](openspec/README.md) |
| Hosting the orchestrator with a web UI on Railway and managing workers from it | [railway/railway-report.md](railway/railway-report.md) | [railway/backlog/README.md](railway/backlog/README.md) |
| Two helper coordinators managing three workers each under the project orchestrator | [coordinators/coordinator-report.md](coordinators/coordinator-report.md) | [coordinators/backlog/README.md](coordinators/backlog/README.md) |
| WebMCP tools on three UI pages, scenarios 1:1 from Playwright, measured against Playwright with and without JEV, with a benchmarks dashboard | [webmcp/webmcp-report.md](webmcp/webmcp-report.md) | [webmcp/backlog/README.md](webmcp/backlog/README.md) |
| Knowledge graph usefulness for new features and reworked issues: PR, branch, failure facts and changed files in prior context, lesson rating and retirement | [knowledge-graph/knowledge-graph-report.md](knowledge-graph/knowledge-graph-report.md) | [knowledge-graph/backlog/README.md](knowledge-graph/backlog/README.md) |

## Issue audit against dev, 2026-10-21

Tracking issue: [#497](https://github.com/petarnenov/ultra-factory/issues/497).
Baseline: `211848e06`. This is a static issue-correctness audit, not a live
reproduction, benchmark result, or release-validation record. All 39 issues in the captured open set were reviewed; proposed features and
unreproduced runtime failures remain open. The tracker records verification.

| Issues | Source evidence and disposition |
| --- | --- |
| #208, #216, #227 | `backend/internal/service/coordinatorpilot/{pilot,loop}_test.go` already exercises stores, services and report scheduling with fake process launch/delivery. Live pilot and resource comparison remain open; `deploy/railway/resource-baseline.md` still records an unrun protocol. |
| #407, #412, #416 | Limits, placement, agent profiles, CLI and UI PRs are merged. Prompt PR #450 is still open. Corrected historical baseline wording and obsolete tracker/dependency status. |
| #453 | `observe/remotereports.Observer.pull` drains pending reports before dropping terminated links. Automatic discovery must check unattended transport policy before authenticated enumeration; it cannot invent links for a blocked host whose sessions it has never read. |
| #454 | `remotehost.Entry` has no per-host HTTP flag. Desktop registry writes use Electron `remotes:*` IPC. `/remote-worker-links/check` is POST spawn preflight, not GET host status. Identity stays an opaque host ID and API version. |
| #455 | `remotehost.FilePath` already shares the default production/dev registry and supports `AO_REMOTES_FILE`; custom installs remain isolated. Migration, cross-process locking and per-puller delivery are separate outstanding work. |
| #456 | The form already checks dirty state. Strict project decoding still discards detailed errors. The rejected field and reported desktop failure are not reproduced by this audit. |
| #468 | Kill and exit map shared operation-gate contention to operation-specific error names. Deferred release exists on admitted paths; a leaked gate is still a hypothesis. |
| #491 | Stored review comments can be stale. Resolution filters the signature input set; it is incorrect to claim that resolution can never change the signature. A signature cannot retract an already queued message. |
| #492 | Local and remote `AssignOwner` return after the store write without sending ownership notices. The missing-notification work remains supported by the code. |
| #493 | Every batch containing stuck reports enters the interrupt path. Mixed batches can fail too; only needs-input bypasses the occupied-window deferral. Corrected the overly narrow stuck-only explanation. |
| #494 | Desktop remote-host IPC requires cloud sign-in; the Go saved-host reader does not. Tailnet-native discovery/auth is proposed work and changes existing listener/auth contracts. |
| #495 | Root creation/host-allocation guidance is absent on dev and overlaps open prompt PR #450. A prompt fixture alone does not prove model behavior. |
| #496 | `limitsMessage` already includes host slots and slot-only changes send a notice. Reliable delivery and observer-driven merge notification remain separate requirements. |

The WebMCP backlog corrections include ten catalog scenarios rather than nine,
a boolean preview gate, host-scoped merged conversation state, phase-specific
JEV arguments/assertions, and explicit timing/failure statistics. Refer to the
individual task blueprints for the corrected contracts. No implementation of
those proposed features is claimed by this audit.

### WebMCP audit coverage

| Issues | Evidence and disposition |
| --- | --- |
| #469, #470, #471, #472 | No renderer WebMCP implementation exists on the baseline. `main.tsx` and boolean `preview-mode.ts` are the integration points. Kept the all-build registration/dev-only call-surface plan, corrected gating, and specified registration cleanup, native adapter and production runtime checks. |
| #473, #474, #475 | `SessionsBoard`, `SettingsProvider`, settings catalog/update hooks, `useConversation`, and composer/retry handlers establish actual lifecycle, state and guard boundaries. Corrected cache assumptions, host scope, settings-open gates, subscriptions and test selection. |
| #476, #477, #478 | All ten named source scenarios exist in the six cited e2e files. Corrected the count, phase/action arguments and parent schema vocabulary. |
| #479, #480, #481 | Compared proposed drivers with `playwright.config.ts`, `chat-performance.spec.ts` and the source scenario sequences. Corrected timing, failure retention and phase assertions; JEV calls remain opt-in and historical probe numbers are not new measurements. |
| #482, #483, #484, #485 | No committed benchmark dataset or summarizer exists on the baseline. Corrected record/aggregation consistency, unique run identities, nullable unsuccessful measurements, check-only semantics and non-disclosing secret checks. |
| #486, #487, #488, #489, #490 | Existing routes/sidebar/palette, theme tokens and performance-report template provide integration patterns; no benchmark route/chart primitive exists. Corrected dependencies, metadata provenance, missing-data handling and statistical acceptance. |

Validation is documentation-specific: issue/body read-back, exact mirror
comparison, all 39 issues' required type/priority labels, ten catalog entries,
referenced local integration files, whitespace checks and pushed Git SHA.
Backend/frontend suites and live benchmarks were not run: executable code did
not change. This audit does not certify future implementations or runtime
reproductions and does not mark their acceptance checklists complete.
