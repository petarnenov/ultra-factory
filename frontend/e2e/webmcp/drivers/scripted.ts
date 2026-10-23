/// <reference path="../../../src/renderer/webmcp/global.d.ts" />
import { expect, type Page } from "@playwright/test";
import type { ToolErrorCode } from "../../../src/renderer/webmcp/model-context";
import { runExpect, ToolCallError, waitForTools } from "../assert";
import type { ToolStep } from "../scenarios";
import { errorMessage, type Driver, type StepTiming } from "./types";

export const scripted: Driver = async (page, host, s) => {
	await waitForTools(page, `${s.page}.`);
	const startedAt = performance.now();
	let bridgeMs = 0;
	let assertMs = 0;
	const steps: StepTiming[] = [];
	const result = (error?: unknown) => ({
		pass: error === undefined,
		totalMs: performance.now() - startedAt - bridgeMs,
		assertMs,
		steps,
		...(error === undefined ? {} : { error: errorMessage(error) }),
	});
	try {
		for (const [index, step] of s.tools.entries()) {
			const stepStart = performance.now();
			if (step.kind === "bridge") {
				await step.run(page).finally(() => (bridgeMs += performance.now() - stepStart));
			} else if (step.kind === "tool") {
				const execMs = await callTimed(page, step);
				steps.push({ index, tool: step.name, execMs, roundTripMs: performance.now() - stepStart });
			} else {
				await runExpect(page, host, step).finally(() => (assertMs += performance.now() - stepStart));
			}
		}
	} catch (error) {
		return result(error);
	}
	return result();
};

// Like runToolStep, but also returns the in-page execution time.
async function callTimed(page: Page, step: ToolStep): Promise<number> {
	const outcome = await page.evaluate(async ([name, args]) => {
		const start = performance.now();
		try {
			await window.__aoWebMcp!.callTool(name, args);
			return { ok: true as const, execMs: performance.now() - start };
		} catch (error) {
			const execMs = performance.now() - start;
			const { code, message } = error as { code?: ToolErrorCode; message?: string };
			return { ok: false as const, execMs, code, message: message ?? String(error) };
		}
	}, [step.name, step.args ?? {}] as const);
	if (step.rejects) {
		expect(outcome.ok, `${step.name} should reject with ${step.rejects}`).toBe(false);
		expect(outcome.ok ? undefined : outcome.code).toBe(step.rejects);
	} else if (!outcome.ok) {
		throw new ToolCallError(step.name, outcome.code, outcome.message);
	}
	return outcome.execMs;
}
