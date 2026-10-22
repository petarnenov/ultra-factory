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

/** Tokens the coordinator fills in itself, so they are not placeholders. */
const INSTRUCTION_TOKENS = ["<task-id>", "<number>", "<head-sha>"];

/** Left in a task that has no acceptance criteria; blocks Start. */
export const ACCEPTANCE_MARKER = "<acceptance criteria: fill in>";

/** Every placeholder the dialog must fill before the brief can start. */
export const BRIEF_PLACEHOLDERS: readonly string[] = [
	...new Set(
		[...COORDINATOR_BRIEF_TEMPLATE.matchAll(/<[^<>\n]+>/g)]
			.map((match) => match[0])
			.filter((token) => !INSTRUCTION_TOKENS.includes(token)),
	),
	ACCEPTANCE_MARKER,
];

export type BriefTask = { taskId: string; title: string; acceptance: string; files: string };

/** The dialog fields that the brief is rendered from. */
export type BriefFields = {
	name: string;
	scope: string;
	outOfScope: string;
	integrationBranch: string;
	branchPrefix: string;
	tasks: BriefTask[];
};

/** Suggested worker branch prefix for a scope, or "" when there is none. */
export function defaultBranchPrefix(scope: string): string {
	const slug = scope
		.toLowerCase()
		.replace(/[^a-z0-9]+/g, "-")
		.replace(/^-+|-+$/g, "")
		.slice(0, 40)
		.replace(/-+$/, "");
	return slug === "" ? "" : `co-${slug}`;
}

/** Placeholders present in a brief text, in the order they appear. */
export function placeholdersIn(text: string): string[] {
	return BRIEF_PLACEHOLDERS.map((token) => ({ token, index: text.indexOf(token) }))
		.filter((found) => found.index !== -1)
		.sort((a, b) => a.index - b.index)
		.map((found) => found.token);
}

/** The first placeholder still present in a brief, or null when none is. */
export function firstUnfilledPlaceholder(brief: string): string | null {
	return placeholdersIn(brief)[0] ?? null;
}

const TASK_LIST_START = "Task list (work these, nothing else):\n";
const TASK_LIST_END = "\n\nWorker cap:";

// Fills the template from the fields; `unfilled` lists the placeholders left in
// the template or the generated task rows. Task text is user input.
export function renderBrief(fields: BriefFields): { brief: string; unfilled: string[] } {
	const unfilled: string[] = [];
	const value = (text: string, placeholder: string) => {
		const trimmed = text.trim();
		if (trimmed !== "") return trimmed;
		unfilled.push(placeholder);
		return placeholder;
	};
	const prefix = fields.branchPrefix.trim() || defaultBranchPrefix(fields.scope);
	const from = COORDINATOR_BRIEF_TEMPLATE.indexOf(TASK_LIST_START) + TASK_LIST_START.length;
	const to = COORDINATOR_BRIEF_TEMPLATE.indexOf(TASK_LIST_END);
	// Function replacers keep "$" in user text out of the pattern syntax.
	const fill = (text: string) =>
		text
			.replace("<coordinator name>", () => value(fields.name, "<coordinator name>"))
			.replace("<one bounded area, at most 200 characters; same text as --scope>", () =>
				value(fields.scope, "<one bounded area, at most 200 characters; same text as --scope>"),
			)
			.replace("<areas and files this coordinator must not touch>", () =>
				value(fields.outOfScope, "<areas and files this coordinator must not touch>"),
			)
			.replace("<dev>", () => value(fields.integrationBranch, "<dev>"))
			.replace("<prefix>", () => value(prefix, "<prefix>"));
	const headFilled = fill(COORDINATOR_BRIEF_TEMPLATE.slice(0, from));
	const rows = fields.tasks.length > 0 ? fields.tasks : [{ taskId: "", title: "", acceptance: "", files: "" }];
	const taskLines = rows
		.map((task, index) =>
			[
				`${index + 1}. ${value(task.taskId, "<issue or task id>")} - ${value(task.title, "<title>")}`,
				`   Acceptance criteria: ${value(task.acceptance, ACCEPTANCE_MARKER)}`,
				`   Files likely touched: ${task.files.trim() || "none listed"}`,
			].join("\n"),
		)
		.join("\n");
	const tailFilled = fill(COORDINATOR_BRIEF_TEMPLATE.slice(to));
	return { brief: headFilled + taskLines + tailFilled, unfilled };
}
