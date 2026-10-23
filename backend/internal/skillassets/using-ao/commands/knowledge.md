# ao knowledge

Record and search lessons in the shared knowledge graph. Every worker task's
outcome is recorded there automatically; lessons are what workers add on top.

## Syntax

```text
ao knowledge add [--kind pitfall|fix|tip|context] [--topic <area>]... <lesson>
ao knowledge search [--project <id>] [--limit <n>] [--json] <words>
ao knowledge rate <lesson-id> wrong|useful
ao knowledge retire <lesson-id> --reason "<text>"
```

A new worker's task prompt already carries a "Knowledge Graph" section with
earlier attempts on the same issue, ranked lessons, and similar past tasks.
Read it before you start; verify a lesson against the current code before
relying on it.

Before your final `ao report --done` or `ao report --stuck`, record 1-3
lessons a future worker on a similar task would need:

- `pitfall`: something that broke or misled you, and how to spot it.
- `fix`: the change or command that resolved a problem.
- `tip`: a shortcut or convention that saved time.
- `context`: a fact about the codebase or environment that was hard to find.

Write each lesson so it stands alone: name the files, commands, or error
messages involved. Do not record secrets, credentials, or facts that only
matter to this one task.

If a lesson already exists in other words, `ao knowledge add` prints
`Confirmed existing lesson <id>`: your wording is kept and your task counts
once as a confirmation (unless it recorded that lesson itself), so there is
nothing to retry.

Lessons served to a task that later merges rank higher; lessons served to
closed or failed tasks rank lower.

Rate a served lesson by its displayed short id; duplicate ratings from one
session count once. Operators can retire a lesson with a reason. Retired
lessons no longer appear in prompts or searches.

`ao knowledge` answers `501` when this machine has no knowledge graph
configured. That is not a task failure; continue without it.
