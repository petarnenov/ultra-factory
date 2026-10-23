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
- Since #568 (ADR [0014](../../../../../adr/0014-merge-near-identical-lessons.md)),
  `AddLesson` looks up merge candidates with `Graph.SimilarLessons` (same
  project and kind, score ≥ `mergeScore`) and confirms the stored lesson via
  `confirmLessonCypher`. That query sets `l.confirmations` and
  `l.confirmedAt` and keeps each new wording on a
  `(:Task)-[:CONFIRMED {at, texts}]->(:Lesson)` edge.
  `ao knowledge add` then prints `Confirmed existing lesson`. The rank
  formula above is unchanged: `confirmations` does not affect it.
  This task touches the same files (`graph.go`, `service.go`, CLI
  `knowledge.go`, the controller); start from current `dev`.

## Subtasks

- [ ] `ao knowledge rate <lesson-id> wrong|useful` → `POST /knowledge/lessons/{id}/rating`;
  `wrong` increments `l.wrong`, `useful` increments `l.useful`; one rating per
  (session, lesson), stored on the `RECEIVED` edge.
- [ ] `ao knowledge retire <lesson-id> --reason "<text>"` sets `l.retiredAt`
  and `l.retiredReason`; retired lessons are excluded from all lesson queries.
- [ ] Rank becomes `score * (1 + helped + useful) / (1 + hurt + 2*wrong)`.
- [ ] Print a short lesson id on each served lesson line so workers can rate it;
  extend `RecordInstruction` with one sentence about `rate … wrong`.
- [ ] Exclude retired lessons from `SimilarLessons`. Otherwise a new lesson
  that matches a retired one is absorbed into it as a confirmation and never
  served. A new lesson that matches only a retired lesson is stored as new.
- [ ] `rate` applies to the stored lesson, whichever wording the worker saw.
  Do not give `CONFIRMED` its own rating or rank.
- [ ] Keep the rank change to `helped`/`hurt`/`useful`/`wrong`; adding
  `confirmations` to the rank is decided in KG-4.2.1.
- [ ] Update the skill text and `docs/cli` for the new subcommands.
- [ ] Unit tests for service validation, ranking, exclusion and CLI table tests.

## Acceptance criteria

- [ ] Rating twice from the same session counts once.
- [ ] A retired lesson disappears from prompts and `ao knowledge search`.
- [ ] A new lesson near-identical to a retired one is stored as a new lesson,
  not merged into the retired one (unit test on `SimilarLessons` and
  `AddLesson`).
- [ ] The prompt section stays under `maxSectionText`.

## Verification

Unit tests; live test; `ao knowledge rate`/`retire` against the live graph
from a probe worker; `make ci-check`.

## Rollout

Daemon and CLI change; part of the shared rollout.
