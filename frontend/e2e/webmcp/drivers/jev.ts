/// <reference path="../../../src/renderer/webmcp/global.d.ts" />
import { type Page } from "@playwright/test";
import {
	APIError,
	choice,
	RateLimitError,
	TypeSafeClient,
} from "@typesafe-ai/sdk";
import type { BenchStep } from "../../../src/renderer/benchmarks/record";
import { runExpect, ToolCallError, waitForTools } from "../assert";
import type { BenchScenario, Driver, DriverResult } from "./types";
import { errorMessage } from "./types";
import type { HostState, JevAction, Phase } from "../scenarios";

const HTTP_TIMEOUT_MS = 10_000;
const RETRY_WAIT_MS = 500;
const MAX_ACTIONS = 64;
const DONE = "done";
const DECISION_ID = "next_action";
const INSTRUCTIONS = "Choose the next catalog action needed to complete the current phase, or done if all required phases and checks are complete.";
const CRITERIA = "Select the catalog action that advances the stated goal. Choose done only after all required phases and checks have passed.";

type DecisionAnswer = { choice: string; confidence: number; probabilities: Record<string, number> };
type DecisionResult = {
	model: string;
	usage: { input_tokens: number | null; output_tokens: number | null };
	answers: Record<string, DecisionAnswer>;
};
type ResponsePromise = Promise<DecisionResult> & {
	withResponse?: () => Promise<{ data: DecisionResult; response: Response }>;
};
export type JevClient = { systemOne: (request: unknown) => ResponsePromise };
type Attempt = { status: number | null; ms: number };
type JevMetadata = { model: string; inputTokens: number; outputTokens: number; attempts: Attempt[] };
type JevResult = DriverResult & { jev?: JevMetadata };

export type JevDependencies = {
	createClient: (apiKey: string) => JevClient;
	waitForTools: (page: Page, prefix: string) => Promise<void>;
	toolDescriptions: (page: Page) => Promise<Record<string, string>>;
	callTool: (page: Page, action: JevAction) => Promise<number>;
	runExpect: (page: Page, host: HostState, check: Phase["checks"][number]) => Promise<void>;
	wait: (ms: number) => Promise<void>;
};

function sdkClient(apiKey: string): JevClient {
	return new TypeSafeClient({ apiKey, retry: { maxRetries: 0 }, timeout: HTTP_TIMEOUT_MS, logLevel: "off" }) as unknown as JevClient;
}

function browserCall(page: Page, action: JevAction): Promise<number> {
	return page.evaluate(async ([tool, args]) => {
		const start = performance.now();
		try {
			await window.__aoWebMcp!.callTool(tool, args);
			return { ok: true as const, execMs: performance.now() - start };
		} catch (error) {
			const { code, message } = error as { code?: string; message?: string };
			return { ok: false as const, execMs: performance.now() - start, code, message: message ?? String(error) };
		}
	}, [action.tool, action.args] as const).then((outcome) => {
		if (!outcome.ok) throw Object.assign(new ToolCallError(action.tool, outcome.code as never, outcome.message), { execMs: outcome.execMs });
		return outcome.execMs;
	});
}

function browserToolDescriptions(page: Page): Promise<Record<string, string>> {
	return page.evaluate(() => Object.fromEntries(window.__aoWebMcp!.listTools().map((tool) => [tool.name, tool.description])));
}

const defaults: JevDependencies = {
	createClient: sdkClient,
	waitForTools,
	toolDescriptions: browserToolDescriptions,
	callTool: browserCall,
	runExpect,
	wait: (ms) => new Promise((resolve) => setTimeout(resolve, ms)),
};

export const jevSkipReason = "JEV driver requires AO_WEBMCP_BENCH=1 and TYPESAFE_API_KEY or JEV_MAF_LAB";
export const jevApiKey = (): string | undefined => {
	const value = [process.env.TYPESAFE_API_KEY, process.env.JEV_MAF_LAB].find((candidate) => candidate?.trim());
	return value?.trim();
};
export const jevAvailable = process.env.AO_WEBMCP_BENCH === "1" && jevApiKey() !== undefined;

function statusOf(error: unknown): number | null {
	return error instanceof APIError ? error.status : null;
}

function retryable(error: unknown): boolean {
	return error instanceof RateLimitError || (error instanceof APIError && error.status >= 500 && error.status <= 599);
}

function choicesFor(actions: JevAction[]): Record<string, string> {
	return Object.fromEntries([...actions.map((action) => [action.id, CRITERIA] as const), [DONE, CRITERIA] as const]);
}

function stateFor(s: BenchScenario, phase: Phase, actions: JevAction[], descriptions: Record<string, string>): string {
	const visible = actions.map((action) => ({
		id: action.id,
		tool: action.tool,
		description: descriptions[action.tool] ?? action.tool,
		fixedArguments: action.args,
	}));
	return JSON.stringify({ goal: s.jevGoal, scenario: s.id, phase: phase.id, catalogActions: visible, done: DONE });
}

async function withOneRetry(
	client: JevClient,
	state: string,
	allowed: Record<string, string>,
	deps: JevDependencies,
): Promise<{ response: DecisionResult; attempts: Attempt[]; decisionMs: number }> {
	const attempts: Attempt[] = [];
	const decisionStarted = performance.now();
	for (let attempt = 0; attempt < 2; attempt++) {
		const attemptStarted = performance.now();
		try {
			const pending = client.systemOne({ state, questions: { [DECISION_ID]: choice(INSTRUCTIONS, allowed) } });
			const received = pending.withResponse ? await pending.withResponse() : { data: await pending, response: null };
			attempts.push({ status: received.response?.status ?? null, ms: performance.now() - attemptStarted });
			return { response: received.data, attempts, decisionMs: performance.now() - decisionStarted };
		} catch (error) {
			attempts.push({ status: statusOf(error), ms: performance.now() - attemptStarted });
			if (attempt !== 0 || !retryable(error)) throw Object.assign(error instanceof Error ? error : new Error(errorMessage(error)), { attempts, decisionMs: performance.now() - decisionStarted });
			await deps.wait(RETRY_WAIT_MS);
		}
	}
	throw new Error("unreachable JEV retry state");
}

function actionLimit(s: BenchScenario): number {
	return Math.min(MAX_ACTIONS, Math.max(1, s.jevActions.length + 1));
}

export async function runJev(
	page: Page,
	host: HostState,
	s: BenchScenario,
	client: JevClient,
	deps: JevDependencies = defaults,
): Promise<JevResult> {
	const preparationStartedAt = performance.now();
	let descriptions: Record<string, string>;
	try {
		await deps.waitForTools(page, `${s.page}.`);
		descriptions = await deps.toolDescriptions(page);
	} catch (error) {
		return { pass: false, totalMs: performance.now() - preparationStartedAt, assertMs: 0, steps: [], error: errorMessage(error), jev: { model: "unknown", inputTokens: 0, outputTokens: 0, attempts: [] } };
	}
	const startedAt = performance.now();
	let bridgeMs = 0;
	let assertMs = 0;
	let lastModel = "unknown";
	let inputTokens = 0;
	let outputTokens = 0;
	const attempts: Attempt[] = [];
	const steps: BenchStep[] = [];
	const completed = new Set<string>();
	const completedPhases = new Set<string>();
	const maxActions = actionLimit(s);
	let actionDecisions = 0;
	let failure: unknown;
	const result = (): JevResult => ({
		pass: failure === undefined,
		totalMs: performance.now() - startedAt - bridgeMs,
		assertMs,
		steps,
		jev: { model: lastModel, inputTokens, outputTokens, attempts: [...attempts] },
		...(failure === undefined ? {} : { error: errorMessage(failure) }),
	});

	try {
		for (const phase of s.phases) {
			if (phase.bridge) {
				const bridgeStart = performance.now();
				try {
					await phase.bridge.run(page);
				} finally {
					bridgeMs += performance.now() - bridgeStart;
				}
			}
			if (phase.actions.length === 0) {
				for (const check of phase.checks) {
					const assertStart = performance.now();
					try {
						await deps.runExpect(page, host, check);
					} finally {
						assertMs += performance.now() - assertStart;
					}
				}
				completedPhases.add(phase.id);
				continue;
			}
			const phaseDecisions = phase.actions.length;
			for (let i = 0; i < phaseDecisions && actionDecisions < maxActions; i++) {
				actionDecisions++;
				const allowedActions = s.jevActions.filter((action) => action.phase === phase.id && !completed.has(action.id));
				const decisionStart = performance.now();
				let selected: string | undefined;
				let confidence: number | undefined;
				let decisionMs = 0;
				let action: JevAction | undefined;
				try {
					const decision = await withOneRetry(client, stateFor(s, phase, allowedActions, descriptions), choicesFor(allowedActions), deps);
					attempts.push(...decision.attempts);
					decisionMs = decision.decisionMs;
					lastModel = decision.response.model;
					inputTokens += decision.response.usage.input_tokens ?? 0;
					outputTokens += decision.response.usage.output_tokens ?? 0;
					const answer = decision.response.answers[DECISION_ID];
					selected = answer?.choice;
					confidence = answer?.confidence;
					action = allowedActions.find((candidate) => candidate.id === selected);
					steps.push({ index: steps.length, tool: action?.tool ?? selected ?? "<invalid>", execMs: 0, roundTripMs: 0, decisionMs, choice: selected, confidence, probabilities: answer?.probabilities, matchedScripted: action !== undefined && s.jevActions[steps.length]?.id === selected });
				} catch (error) {
					attempts.push(...((error as Error & { attempts?: Attempt[] }).attempts ?? []));
					decisionMs = (error as Error & { decisionMs?: number }).decisionMs ?? performance.now() - decisionStart;
					steps.push({ index: steps.length, tool: "<decision-error>", execMs: 0, roundTripMs: 0, decisionMs });
					throw error;
				}

				if (selected === DONE) {
					if (completedPhases.size !== s.phases.length) throw new Error(`JEV chose done before all required phases passed (${completedPhases.size}/${s.phases.length})`);
					return result();
				}
				if (!action) throw new Error(`JEV chose invalid action ${String(selected)}`);
				if (action.phase !== phase.id) throw new Error(`JEV chose action ${action.id} outside phase ${phase.id}`);
				if (completed.has(action.id)) throw new Error(`JEV repeated catalog action ${action.id}`);
				const stepRecord = steps[steps.length - 1];
				const callStart = performance.now();
				try {
					stepRecord.execMs = await deps.callTool(page, action);
					stepRecord.roundTripMs = performance.now() - callStart;
					if (action.rejects) throw new Error(`Expected ${action.tool} to reject with ${action.rejects}`);
				} catch (error) {
					stepRecord.execMs = (error as Error & { execMs?: number }).execMs ?? stepRecord.execMs;
					stepRecord.roundTripMs = performance.now() - callStart;
					if (!action.rejects || !(error instanceof ToolCallError) || error.code !== action.rejects) throw error;
				}
				completed.add(action.id);
				if (phase.actions.every((candidate) => completed.has(candidate.id))) {
					for (const check of phase.checks) {
						const assertStart = performance.now();
						try {
							await deps.runExpect(page, host, check);
						} finally {
							assertMs += performance.now() - assertStart;
						}
					}
					completedPhases.add(phase.id);
					break;
				}
			}
			if (!completedPhases.has(phase.id)) throw new Error(`JEV action cap reached in phase ${phase.id}`);
		}
		const terminalStart = performance.now();
		try {
			const terminal = await withOneRetry(client, JSON.stringify({ goal: s.jevGoal, scenario: s.id, phase: "complete", catalogActions: [], done: DONE }), choicesFor([]), deps);
			attempts.push(...terminal.attempts);
			lastModel = terminal.response.model;
			inputTokens += terminal.response.usage.input_tokens ?? 0;
			outputTokens += terminal.response.usage.output_tokens ?? 0;
			const answer = terminal.response.answers[DECISION_ID];
			steps.push({ index: steps.length, tool: answer?.choice ?? "<invalid>", execMs: 0, roundTripMs: 0, decisionMs: terminal.decisionMs, choice: answer?.choice, confidence: answer?.confidence, probabilities: answer?.probabilities, matchedScripted: answer?.choice === DONE });
			if (answer?.choice !== DONE) throw new Error(`JEV chose invalid terminal action ${String(answer?.choice)}`);
		} catch (error) {
			attempts.push(...((error as Error & { attempts?: Attempt[] }).attempts ?? []));
			const decisionMs = (error as Error & { decisionMs?: number }).decisionMs ?? performance.now() - terminalStart;
			if (steps.length === 0 || steps[steps.length - 1].decisionMs === undefined) steps.push({ index: steps.length, tool: "<decision-error>", execMs: 0, roundTripMs: 0, decisionMs });
			throw error;
		}
		return result();
	} catch (error) {
		failure = error;
		return result();
	}
}

export const jev: Driver = async (page, host, s) => {
	const apiKey = jevApiKey();
	if (process.env.AO_WEBMCP_BENCH !== "1" || !apiKey) throw new Error(jevSkipReason);
	try {
		const result = await runJev(page, host, s, defaults.createClient(apiKey), defaults);
		if (result.error) result.error = result.error.replaceAll(apiKey, "[redacted]");
		return result;
	} catch (error) {
		return { pass: false, totalMs: 0, assertMs: 0, steps: [], error: errorMessage(error).replaceAll(apiKey, "[redacted]"), jev: { model: "unknown", inputTokens: 0, outputTokens: 0, attempts: [] } };
	}
};
