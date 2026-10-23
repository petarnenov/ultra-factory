import { expect, test, type Page } from "@playwright/test";
import { RateLimitError } from "@typesafe-ai/sdk";
import { runJev, type JevClient, type JevDependencies } from "./webmcp/drivers/jev";
import type { BenchScenario } from "./webmcp/drivers/types";
import { createHostState, scenarios } from "./webmcp/scenarios";

function stubScenario(): BenchScenario {
	const source = scenarios.find((scenario) => scenario.id === "settings.sections");
	if (!source) throw new Error("settings.sections scenario is missing");
	return source;
}

function scriptedClient(choices: string[], onCall?: (request: unknown) => void, fallback = "done"): JevClient {
	return {
		systemOne: (request) => {
			onCall?.(request);
			const selected = choices.shift() ?? fallback;
			const data = {
				model: "jev-test",
				usage: { input_tokens: 10, output_tokens: 2 },
				answers: { next_action: { choice: selected, confidence: 0.9, probabilities: { [selected]: 0.9 } } },
			};
			return Object.assign(Promise.resolve(data), {
				withResponse: async () => ({ data, response: new Response(null, { status: 200 }) }),
			});
		},
	};
}

function deps(overrides: Partial<JevDependencies> = {}): JevDependencies {
	return {
		createClient: () => scriptedClient([]),
		waitForTools: async () => {},
		toolDescriptions: async () => ({}),
		callTool: async () => 1,
		runExpect: async () => {},
		wait: async () => {},
		...overrides,
	};
}

test("JEV follows phase action IDs and records terminal decision metadata", async () => {
	const scenario = stubScenario();
	const choices = scenario.jevActions.map((action) => action.id).concat("done");
	const calls: unknown[] = [];
	const result = await runJev({} as Page, createHostState(), scenario, scriptedClient(choices, (request) => calls.push(request)), deps());

	expect(result.pass).toBe(true);
	expect(result.steps.map((step) => step.choice)).toEqual(scenario.jevActions.map((action) => action.id).concat("done"));
	expect(result.steps.at(-1)?.choice).toBe("done");
	expect(result.steps.every((step) => step.decisionMs !== undefined)).toBe(true);
	expect(result.steps.every((step) => step.execMs !== undefined && step.roundTripMs !== undefined)).toBe(true);
	expect(result.jev).toMatchObject({ model: "jev-test", inputTokens: 30, outputTokens: 6 });
	expect(calls).toHaveLength(3);
	expect(JSON.parse((calls[0] as { state: string }).state)).toMatchObject({
		catalogActions: [{ id: "select-updates", fixedArguments: { id: "updates" } }],
	});
});

test("JEV records early done as an incomplete phase failure", async () => {
	const result = await runJev({} as Page, createHostState(), stubScenario(), scriptedClient(["done"]), deps());

	expect(result.pass).toBe(false);
	expect(result.error).toContain("before all required phases passed");
	expect(result.steps).toHaveLength(1);
	expect(result.steps[0].choice).toBe("done");
	expect(result.jev?.attempts).toHaveLength(1);
});

test("JEV retains a timed terminal decision error after successful actions", async () => {
	const scenario = stubScenario();
	const scripted = scriptedClient(scenario.jevActions.map((action) => action.id));
	let calls = 0;
	const client: JevClient = {
		systemOne: (request) => {
			calls++;
			if (calls > scenario.jevActions.length) throw new Error("terminal request failed");
			return scripted.systemOne(request);
		},
	};
	const result = await runJev({} as Page, createHostState(), scenario, client, deps());

	expect(result.pass).toBe(false);
	expect(result.error).toBe("terminal request failed");
	expect(result.steps.map((step) => step.choice)).toEqual([...scenario.jevActions.map((action) => action.id), undefined]);
	expect(result.steps.at(-1)).toMatchObject({
		index: scenario.jevActions.length,
		tool: "<decision-error>",
		execMs: 0,
		roundTripMs: 0,
	});
	expect(result.steps.at(-1)?.decisionMs).toBeGreaterThanOrEqual(0);
	expect(result.jev?.attempts).toHaveLength(scenario.jevActions.length + 1);
});

test("JEV action cap retains partial action evidence", async () => {
	const original = stubScenario();
	const sourcePhase = original.phases[0];
	const actions = Array.from({ length: 65 }, (_, index) => ({
		kind: "tool" as const,
		id: `bounded-${index}`,
		name: "settings.select_section",
		args: { id: "updates" },
	}));
	const scenario: BenchScenario = {
		...original,
		phases: [{ ...sourcePhase, actions }, ...original.phases.slice(1)],
		jevActions: [
			...actions.map((action) => ({ id: action.id, phase: sourcePhase.id, tool: action.name, args: action.args })),
			...original.jevActions.filter((action) => action.phase !== sourcePhase.id),
		],
	};
	const result = await runJev(
		{} as Page,
		createHostState(),
		scenario,
		scriptedClient(scenario.jevActions.map((action) => action.id)),
		deps({ callTool: async () => 3 }),
	);

	expect(result.pass).toBe(false);
	expect(result.error).toContain("action cap reached");
	expect(result.steps[0].execMs).toBe(3);
	expect(result.steps[0].roundTripMs).toBeGreaterThanOrEqual(0);
	expect(result.steps).toHaveLength(64);
});

test("JEV retries one 429 attempt and records both statuses", async () => {
	const scenario = stubScenario();
	let calls = 0;
	const client: JevClient = {
		systemOne: async () => {
			calls++;
			if (calls === 1) throw new RateLimitError(429, {}, new Headers());
			return {
				model: "jev-test",
				usage: { input_tokens: 1, output_tokens: 1 },
				answers: { next_action: { choice: "done", confidence: 1, probabilities: { done: 1 } } },
			};
		},
	};
	const result = await runJev({} as Page, createHostState(), scenario, client, deps({ wait: async () => {} }));

	expect(result.pass).toBe(false);
	expect(calls).toBe(2);
	expect(result.jev?.attempts.map((attempt) => attempt.status)).toEqual([429, null]);
	expect(result.steps).toHaveLength(1);
});

test("JEV retains null HTTP status and the original decision failure", async () => {
	const scenario = stubScenario();
	const client: JevClient = { systemOne: async () => { throw new Error("connection closed"); } };
	const result = await runJev({} as Page, createHostState(), scenario, client, deps());

	expect(result.pass).toBe(false);
	expect(result.error).toBe("connection closed");
	expect(result.jev?.attempts).toEqual([{ status: null, ms: expect.any(Number) }]);
	expect(result.steps).toHaveLength(1);
});

test("JEV assertion failure keeps the executed action and assertion time", async () => {
	const scenario = stubScenario();
	const result = await runJev(
		{} as Page,
		createHostState(),
		scenario,
		scriptedClient(scenario.jevActions.map((action) => action.id)),
		deps({ runExpect: async () => { throw new Error("phase assertion failed"); } }),
	);

	expect(result.pass).toBe(false);
	expect(result.error).toBe("phase assertion failed");
	expect(result.steps).toHaveLength(1);
	expect(result.steps[0].execMs).toBe(1);
	expect(result.assertMs).toBeGreaterThanOrEqual(0);
});
