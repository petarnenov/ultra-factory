# KG-3.1.1 · Task: `ao knowledge rate` and retired lessons

**Status:** ⬜ todo — [#560](https://github.com/petarnenov/ultra-factory/issues/560)

**Type:** enhancement (daemon, CLI). **Estimate:** 0.75 pd. **Story:** KG-3.1.
**Order:** 5 of 8. **Depends on:** none. **Next:** shared rollout, then KG-3.1.2 (#561).

## Goal

A worker can mark a served lesson as wrong; an operator can retire a lesson.
Both change what workers are served.

## Context (verified 2026-10-23)

- CLI: `backend/internal/cli/knowledge.go` (`add`, `search`); HTTP:
  `backend/internal/httpd/controllers/knowledge.go`
  (`POST /knowledge/lessons`, `GET /knowledge/search`); service:
  `knowledgegraph/service.go`.
- Rank today: `score * (1 + helped) / (1 + hurt)` in `lessonsCypher` and
  `lessonsVectorCypher` (`graph.go`).
- Lesson ids are not printed in the prompt; `formatItem` renders
  kind, source task and text (`format.go`).
- The worker instruction lives in `format.go` `RecordInstruction` and the
  skill text in `backend/internal/skillassets/using-ao/commands/knowledge.md`.

## Subtasks

- [ ] `ao knowledge rate <lesson-id> wrong|useful` → `POST /knowledge/lessons/{id}/rating`;
  `wrong` increments `l.wrong`, `useful` increments `l.useful`; one rating per
  (session, lesson), stored on the `RECEIVED` edge.
- [ ] `ao knowledge retire <lesson-id> --reason "<text>"` sets `l.retiredAt`
  and `l.retiredReason`; retired lessons are excluded from all lesson queries.
- [ ] Rank becomes `score * (1 + helped + useful) / (1 + hurt + 2*wrong)`.
- [ ] Print a short lesson id on each served lesson line so workers can rate it;
  extend `RecordInstruction` with one sentence about `rate … wrong`.
- [ ] Update the skill text and `docs/cli` for the new subcommands.
- [ ] Unit tests for service validation, ranking, exclusion and CLI table tests.

## Acceptance criteria

- [ ] Rating twice from the same session counts once.
- [ ] A retired lesson disappears from prompts and `ao knowledge search`.
- [ ] The prompt section stays under `maxSectionText`.

## Verification

Unit tests; live test; `ao knowledge rate`/`retire` against the live graph
from a probe worker; `make ci-check`.

## Rollout

Daemon and CLI change; part of the shared rollout.
