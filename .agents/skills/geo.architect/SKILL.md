---
name: geo.architect
description: Resolve complex architecture decisions through three independent consuls, mutual critique and explicit votes. Use when a root agent or subagent requests an architecture council, before escalating a complex technical issue to a human, or after 15 minutes without a relevant human response. Not for routine fixes or implementing the decision.
---

# Architecture consuls

Return an evidence-based technical decision to the invoking agent, including a subagent. The caller orchestrates and resumes implementation within its existing mandate; consuls only analyze. Never recursively invoke this skill or delegate from a consul.

## Establish the case

Infer the project, branch and case from the caller or current checkout; ask only when a required input cannot be resolved unambiguously. Capture the caller's return channel, constraints, prior decisions and acceptance criteria. For an existing human escalation, read [references/escalation.md](references/escalation.md) before monitoring or takeover. Ordinary pre-escalation consultation starts immediately; it does not wait 15 minutes.

Read the project's AGENTS.md and execution/delegation guidance. Select an allowed host before creating checkouts or launching processes. For ultra-factory, consult `docs/agents/orchestration.md`: the Mac orchestrates and consuls run on an eligible paired worker. Never stop someone else's orchestrator to free a host.

Resolve the requested branch with `git rev-parse --verify '<branch>^{commit}'`; do not silently fetch or switch branches. Record uncommitted changes, but scope analysis to the pinned commit unless the case includes them. If needed, obtain a caller-provided patch, record its digest and apply the identical patch to each isolated checkout before analysis. Identify required untracked inputs explicitly; a commit or ordinary diff does not include them.

## Prepare and launch

Create a unique consultation directory directly in the temporary file archive, outside the repository: `~/.ao/architecture-consults/archive/<YYYY-MM-DD>/<consultation-id>/` for AO, or `architecture-consults/archive/<YYYY-MM-DD>/<consultation-id>/` under the applicable agent state root elsewhere. Use the UTC creation date and a collision-resistant ID. Keep this path stable through delivery and session closure. Read [references/archive-and-close.md](references/archive-and-close.md) for the archive layout and mandatory finalization.

| Artifact | Contents |
| --- | --- |
| `request.md` | Case, project, branch, caller, constraints, acceptance criteria and patch scope |
| `state.json` | ID, explicit `requester` display string, owner, host, canonical project path, SHA, patch digest, phase, caller channel, live handles, exits, escalation, archive, delivery and per-consul closure state |
| `independent/<name>.md` | Frozen first-round analysis |
| `critique/<name>.md` | Frozen mutual critique |
| `proposal-<n>.md` | Immutable proposal version with stable decision IDs |
| `ballots/round-<n>/<name>.md` | Explicit votes on that exact proposal version |
| `final.md`, `logs/` | Delivered decision and separate process diagnostics |
| `deepseek.md`, `astra.md`, `fable.md` | Per-architect collected reports, critiques and ballots, with round/version labels |

For every new consultation, write an explicit `requester` string in `state.json` at creation, using the requester identity supplied by the caller. Follow the requester allowlist in [references/archive-and-close.md](references/archive-and-close.md); do not rely on request prose or owner/session metadata for this field.

Reuse existing results only for the same project, SHA, patch, case, constraints and acceptance criteria. Verify live handles before resuming; files alone do not prove a running consultation. Never duplicate a live council.

Read [references/launch.md](references/launch.md) for preflight, CLI commands, read-only enforcement and failure handling. Verify all three harnesses and exact model access before launching a round. Create three separate detached worktrees at the pinned SHA (`git worktree add --detach <unique-path> <sha>`), confirm their HEADs and any patch match, and preserve one checkout per consul across rounds. Keep artifacts on their owning host and transfer round inputs explicitly when necessary.

Start exactly these three logical consuls concurrently; no provider, harness or model substitutions:

| Consul | Harness | Model | Effort |
| --- | --- | --- | --- |
| deepseek | OpenCode | `opencode/deepseek-v4.1-flash` | provider default |
| astra | Codex | `gpt-6-astra` | `medium` |
| fable | Claude Code | `claude-fable-5-1` | `medium` |

Every round receives the same request, SHA, patch scope and its required frozen artifacts. Consuls must not edit source, commit, merge, deploy or run destructive commands. The caller captures final text atomically after successful exit and semantic inspection; logs, empty output and partial streams are not reports. Monitor actual handles at intervals up to 60 seconds. If any consul fails, retain successful results but keep the round incomplete; two results never replace three.

## Deliberate

1. **Independent analysis.** Hide other consuls' answers. Each inspects project instructions and relevant code, then reports verified facts with paths/lines, assumptions, the architectural problem, viable options (at least two where applicable), recommendation, tradeoffs, affected boundaries, implementation outline, acceptance checks, migration/rollback and unresolved risks. Await all three successful reports.
2. **Mutual critique.** Give each consul all three labeled analyses. Each evaluates every recommendation, identifies evidence, omissions and contradictions, distinguishes facts from speculation, and returns changed conclusions and a ranked recommendation. Await all three critiques. The caller then writes `proposal-1.md` with stable decision IDs, selected options, implementation steps, checks, risks and disagreements; synthesis must not invent agreement.
3. **Ratification.** Give each consul all analyses, critiques and the same immutable proposal. Require APPROVE or REJECT for every decision ID, the proposal version, reasons, evidence and any required amendment. Missing IDs or ambiguous responses are incomplete ballots, not abstentions or approvals. Require all three complete ballots.

A decision passes with at least two approvals and no unresolved objection demonstrating a violated hard requirement. Majority cannot override a demonstrated correctness, security or project-contract defect. Classify and explain blocking objections using evidence, not voter count.

If a decision fails or has a substantiated blocker, write `proposal-2.md` and obtain all three final ballots on that version, retaining the earlier artifacts. Cap ratification at two rounds. Report unresolved decisions with the best-supported actionable recommendation, dissent, missing evidence and the exact blocker; never call majority approval unanimous consensus. A recommendation awaiting votes is not a ratified decision.

Changes to the SHA, patch, case or human constraints invalidate affected conclusions and votes. Do not carry approvals to an altered proposal. Re-review the affected evidence before voting; if the two-round budget is exhausted, return unresolved with the new constraints instead of looping. Human preference gaps may allow a reversible default within the caller's mandate; credentials, external facts and required authorization cannot be supplied by voting or elapsed time.

## Return the decision

Write `final.md`, finish and verify the archive, then deliver its complete decision summary and permanent archive location through the recorded caller channel. Include:

- Consultation ID, project, branch, SHA, patch scope, exact team and resolved/unresolved status.
- Decision and rationale, implementation plan, acceptance checks and rollback.
- Vote matrix per decision and proposal version, material dissent, risks and blockers.
- Paths to all analyses, critiques, proposals and ballots.
- Escalation/takeover timestamps when applicable and whether implementation is authorized.

Mark resolved only when every required decision passes the final applicable ballots and no hard blocker remains. Confirm delivery through the caller mechanism; a file alone is not delivery. If that mechanism is unavailable, return the report directly with the caller identity and delivery limitation. After delivery, close all three consul sessions and verify their terminal state as described in [references/archive-and-close.md](references/archive-and-close.md). Do this for unresolved reports too; a returned report must not leave idle consuls running. If the final response ends the caller turn, verify the archive and close the consuls immediately before sending it. Preserve the archive and existing artifact filenames; session closure must never delete reports or session history. Retain worktrees for review and clean only consultation-owned worktrees once no longer needed, never force-remove dirty worktrees.
