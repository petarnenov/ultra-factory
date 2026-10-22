import { describe, expect, test } from "vitest";
import {
	BRIEF_PLACEHOLDERS,
	COORDINATOR_BRIEF_TEMPLATE,
	defaultBranchPrefix,
	firstUnfilledPlaceholder,
	renderBrief,
} from "./brief-template";

const filled = {
	name: "Ada",
	scope: "auth module",
	outOfScope: "billing",
	integrationBranch: "dev",
	branchPrefix: "",
	tasks: [{ taskId: "#503", title: "Start brief", acceptance: "no placeholder", files: "" }],
};

describe("brief placeholders", () => {
	test("lists the fill-in placeholders and keeps the instruction tokens out", () => {
		expect(BRIEF_PLACEHOLDERS).toEqual(
			expect.arrayContaining(["<coordinator name>", "<dev>", "<prefix>", "<issue or task id>", "<acceptance criteria: fill in>"]),
		);
		expect(BRIEF_PLACEHOLDERS).not.toContain("<task-id>");
		expect(BRIEF_PLACEHOLDERS).not.toContain("<number>");
		expect(BRIEF_PLACEHOLDERS).not.toContain("<head-sha>");
	});

	test("names the first unfilled placeholder in brief order", () => {
		expect(firstUnfilledPlaceholder(COORDINATOR_BRIEF_TEMPLATE)).toBe("<coordinator name>");
		expect(firstUnfilledPlaceholder("Assignment for Ada.\n\nScope: <one bounded area, at most 200 characters; same text as --scope>")).toBe(
			"<one bounded area, at most 200 characters; same text as --scope>",
		);
	});

	test("reports nothing once every placeholder is filled", () => {
		expect(firstUnfilledPlaceholder(renderBrief({ ...filled, branchPrefix: "co-x" }))).toBeNull();
	});
});

describe("renderBrief", () => {
	test("substitutes name, scope, out of scope, integration branch and prefix", () => {
		const brief = renderBrief(filled);
		expect(brief).toContain("Assignment for Ada.");
		expect(brief).toContain("Scope: auth module\n");
		expect(brief).toContain("Out of scope: billing\n");
		expect(brief).toContain("- Integration branch / PR base: dev\n");
		expect(brief).toContain("- Worker branch name: co-auth-module/<task-id>\n");
	});

	test("uses the explicit branch prefix when one is given", () => {
		expect(renderBrief({ ...filled, branchPrefix: "feat/auth" })).toContain("Worker branch name: feat/auth/<task-id>");
	});

	test("renders each task with its acceptance criteria", () => {
		const brief = renderBrief({
			...filled,
			tasks: [
				{ taskId: "#1", title: "First", acceptance: "a", files: "src/a.ts" },
				{ taskId: "#2", title: "Second", acceptance: "", files: "" },
			],
		});
		expect(brief).toContain("1. #1 - First\n   Acceptance criteria: a\n   Files likely touched: src/a.ts\n");
		expect(brief).toContain("2. #2 - Second\n   Acceptance criteria: <acceptance criteria: fill in>\n   Files likely touched: none listed\n");
		expect(brief).toContain("Worker cap: 3 active workers");
	});

	test("leaves the placeholder when a field is blank", () => {
		const brief = renderBrief({ ...filled, name: "  ", integrationBranch: "" });
		expect(firstUnfilledPlaceholder(brief)).toBe("<coordinator name>");
		expect(brief).toContain("Integration branch / PR base: <dev>");
	});

	test("keeps dollar signs in user text literal", () => {
		expect(renderBrief({ ...filled, name: "$& $1" })).toContain("Assignment for $& $1.");
	});

	test("keeps placeholder-like text in task fields literal", () => {
		const brief = renderBrief({
			...filled,
			tasks: [{ taskId: "#7", title: "Rename <prefix> and <dev> handling", acceptance: "tokens stay", files: "src/<dev>.ts" }],
		});
		expect(brief).toContain("1. #7 - Rename <prefix> and <dev> handling\n   Acceptance criteria: tokens stay\n   Files likely touched: src/<dev>.ts\n");
		expect(brief).toContain("- Integration branch / PR base: dev\n");
		expect(brief).toContain("- Worker branch name: co-auth-module/<task-id>\n");
	});

	test("keeps a task row when the list is empty", () => {
		const brief = renderBrief({ ...filled, tasks: [] });
		expect(firstUnfilledPlaceholder(brief)).toBe("<issue or task id>");
	});
});

describe("defaultBranchPrefix", () => {
	test("derives a co- prefix from the scope", () => {
		expect(defaultBranchPrefix("Auth Module / v2!")).toBe("co-auth-module-v2");
		expect(defaultBranchPrefix("  ")).toBe("");
	});
});
