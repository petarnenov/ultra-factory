# 14. Near-identical lessons merge on write

Date: 2026-10-23
Status: Accepted

## Context

Workers that hit the same problem each record their own lesson with
`ao knowledge add`. Ten paraphrases of one lesson fill the prompt's lesson
slots, crowd out different lessons, and split the `helped` / `hurt` signal
of [ADR 0012](0012-central-knowledge-graph.md) across copies.

Calibration with `bge-m3` ([ADR 0013](0013-semantic-search-with-local-embeddings.md),
Neo4j score `(1 + cos) / 2`) on AO lessons showed:

| Pair | Score |
| --- | --- |
| Paraphrases of one lesson | 0.88 to 0.96 |
| Related lessons on the same subject | 0.72 to 0.81 |
| Contradictions ("use X" / "do not use X", "0.70" / "0.90", "system prompt" / "task prompt") | 0.94 to 0.99 |

A vector score alone therefore cannot tell a duplicate from a contradiction.

## Decision

1. **Embed on write.** With an embedder configured, `AddLesson` embeds the
   lesson synchronously and stores the vector with the new node.
2. **Candidates** are lessons of the same project and kind scoring at least
   `mergeScore` (default 0.94, cos 0.88).
3. **Lexical guard.** A candidate merges only when both texts yield the
   same ordered sequence of literal tokens (code spans, numbers, paths,
   flags, and hyphenated, all-caps, camelCase or snake_case names) and
   negations (English and Bulgarian negators outside literals), so a
   negation that moves to another value also blocks the merge. The guard errs
   towards not merging: a missed merge leaves a duplicate, a wrong one
   hides a contradiction. Plain-word contradictions still pass: swapped
   plain words, antonyms ("enable" / "disable") and spelled-out numbers.
4. **Merge is non-lossy.** The stored lesson keeps its text. The task gets
   one `CONFIRMED {at, texts}` edge that collects each new wording it
   submitted. `confirmations` grows by one per task other than the one
   that learned the lesson. Topics are merged, and earlier attempts on an
   issue list the lessons their tasks learned or confirmed.
5. **Failures never block a lesson.** If the lookup or the confirm fails,
   or the candidate disappeared, the lesson is stored as new with the vector
   already computed. If embedding or storing that vector fails, the embed
   loop backfills it.

## Consequences

- `POST /knowledge/lessons` returns the existing lesson's id with
  `merged: true`; `ao knowledge add` prints `Confirmed existing lesson`.
- A wrong merge loses no text: `CONFIRMED.texts` holds it for review.
- A confirm that commits but times out on the way back is followed by a
  create, leaving one duplicate.
- Two identical lessons written at the same moment can both be created; the
  lookup and the write are separate transactions.
- Lessons stored before this change are not merged retroactively.
- `confirmations` is recorded but does not affect ranking yet.
