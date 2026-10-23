# Launching the three consuls

Use CLI processes on the permitted execution host. These examples describe each round; the caller supplies the round prompt, checkout and artifact paths. Build commands with argument arrays or correct shell quoting. Never interpolate project/case text as shell code.

Check `command -v opencode codex claude` and each CLI's current `--help` on that host. Check the available model catalog or a harmless authenticated model probe. Exact model access is provider/account-specific; an accepted flag is not proof the requested model actually ran. Capture the effective model from session metadata when available, and record when it cannot be independently confirmed. Never silently use a default or fallback model.

The local OpenCode catalog exposes `opencode/deepseek-v4.1-flash`. Repository catalog code identifies `gpt-6-astra` and the Fable 5.1 alias, but remote account availability still requires preflight. Do not dump credentials/configuration while checking access.

## Commands

Run each in its own checkout with a common request and round-specific material. Use a prompt file to avoid shell quoting issues. For example, Python subprocess calls use argument lists and `cwd=checkout`:

```python
# prompt is the full round text; paths are absolute.
opencode_args = [
    "opencode", "run", "--model", "opencode/deepseek-v4.1-flash",
    "--file", prompt_path,
    "Analyze the attached request; return the complete round report as final text.",
]
codex_args = [
    "codex", "exec", "--model", "gpt-6-astra",
    "-c", 'model_reasoning_effort="medium"',
    "--sandbox", "read-only", "--ephemeral",
    "--output-last-message", temporary_report_path, "-",
]
claude_args = [
    "claude", "--print", "--model", "claude-fable-5-1",
    "--effort", "medium", "--permission-mode", "plan",
    "--tools", "Read,Glob,Grep", "--output-format", "json",
]
```

For Codex, send prompt through stdin and collect its explicit last-message file. For Claude, send prompt through stdin, parse the JSON result and verify it is a successful non-error result before publishing its `result` text. For OpenCode, collect output and exit status; strip terminal rendering if present and verify the captured text actually contains the analysis, not just session progress. Use its JSON event mode when supported and extract the final assistant report, retaining raw events in logs.

Launch all three before waiting on any one. Store actual process/session handles, their owning runtime and the supported close/stop mechanism; poll them and record exits separately. Track any managed session separately from its CLI process so both can be closed during finalization. Only publish a report after successful exit and semantic inspection. The prompt explicitly prohibits modifications and recursive delegation. Codex has a read-only sandbox and Claude is limited to file-reading tools; OpenCode must use an available read-only/plan agent or externally enforced read-only workspace when its default agent has write tools. Verify the actual agent configuration, not its name alone. Do not use auto-approval or sandbox bypass flags to get past setup failures.

Across rounds use the same logical three consuls. Resume explicit recorded session IDs where supported; never use an ambiguous `--continue`/`--last`. Fresh CLI processes for the same consul are acceptable when they receive the full immutable round context and previous artifacts. They do not add voters.

## Failures and limits

Before launch verify model selection, authentication, host eligibility, project access and all checkout SHAs. If any consul is unavailable, report the exact failed requirement and keep the consultation incomplete; do not substitute another model or quietly run two. Retry only a demonstrated transient failure once with the same consul/model, preserving logs. Authentication/model/access failures need correction rather than blind retries.

Poll active jobs at intervals up to 60 seconds and keep the caller informed. A slow job is not a failed job. If a process exits unsuccessfully, save its partial output as diagnostic material, not an independent analysis or ballot. If the owning runtime imposes a timeout, record it explicitly and do not infer agreement from missing output. No external network calls, builds, tests or paid model probes are required merely to install this skill; an actual consultation uses the requested model sessions.

After returning the decision, close all three consul sessions under [archive-and-close.md](archive-and-close.md). Keep their reports and history in the verified archive; do not leave sessions open for possible future reuse.
