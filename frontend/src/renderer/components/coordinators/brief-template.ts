/** Five-section assignment brief from the operating contract doc. */
export const COORDINATOR_BRIEF_TEMPLATE = `Assignment for <coordinator name>.

Scope: <one bounded area, at most 200 characters; same text as --scope>
Out of scope: <areas and files this coordinator must not touch>

Task list (work these, nothing else):
1. <issue or task id> - <title>
   Acceptance criteria: <testable criteria>
   Files likely touched: <paths>
2. ...

Worker cap: 3 active workers at once, and you may never exceed it. Wait for
a worker to finish before starting another.

Branch rules:
- Integration branch / PR base: <dev>
- Worker branch name: <prefix>/<task-id>
- Workers open pull requests; they never push to the integration branch.
- No force push, no history rewrite, no commits that name an AI tool.

Reporting cadence: use \`ao coordinator report\`. Send a summary now, on every
meaningful state change, when your scope is done, and whenever you are
blocked. No timer-based reports. One line per worker, not raw conversation.

You never merge and never close issues. A finished worker or a green pull
request means "ready for the root to integrate", not "done".

Ready pull requests: spawn workers with auto-review on (the default for your
workers). After a worker reports done, wait for the AO review verdict on its
current head and for green CI. Report a PR with
\`ao coordinator report --ready-pr <number>@<head-sha>\` only when the review
approved that head, CI is green, and no review thread is unresolved.`;
