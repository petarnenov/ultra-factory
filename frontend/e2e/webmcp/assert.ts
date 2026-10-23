/// <reference path="../../src/renderer/webmcp/global.d.ts" />
import { expect, type Page } from "@playwright/test";
import type { ToolErrorCode } from "../../src/renderer/webmcp/model-context";
import type { ExpectStep, HostState, ToolStep } from "./scenarios";

const TOOLS_TIMEOUT_MS = 15_000;

// A tool call rejected in the page; carries the ToolError code if any.
export class ToolCallError extends Error {
	readonly code: ToolErrorCode | undefined;

	constructor(name: string, code: ToolErrorCode | undefined, message: string) {
		super(`${name}: ${message}`);
		this.name = "ToolCallError";
		this.code = code;
	}
}

export async function callTool(page: Page, name: string, args?: unknown): Promise<unknown> {
	const outcome = await page.evaluate(async ([toolName, toolArgs]) => {
		try {
			return { ok: true as const, value: await window.__aoWebMcp!.callTool(toolName, toolArgs) };
		} catch (error) {
			const { code, message } = error as { code?: ToolErrorCode; message?: string };
			return { ok: false as const, code, message: message ?? String(error) };
		}
	}, [name, args ?? {}] as const);
	if (!outcome.ok) throw new ToolCallError(name, outcome.code, outcome.message);
	return outcome.value;
}

// A step with `rejects` passes only when the tool fails with that code.
export async function runToolStep(page: Page, step: ToolStep): Promise<unknown> {
	if (!step.rejects) return callTool(page, step.name, step.args);
	const error = await callTool(page, step.name, step.args).then(
		() => undefined,
		(rejection: unknown) => rejection,
	);
	expect(error, `${step.name} should reject with ${step.rejects}`).toBeInstanceOf(ToolCallError);
	expect((error as ToolCallError).code).toBe(step.rejects);
	return undefined;
}

export async function listToolNames(page: Page): Promise<string[]> {
	return page.evaluate(() => window.__aoWebMcp!.listTools().map((tool) => tool.name));
}

export async function waitForTools(page: Page, prefix: string): Promise<void> {
	await expect
		.poll(async () => (await listToolNames(page)).some((name) => name.startsWith(prefix)), {
			message: `a tool named ${prefix}* is registered`,
			timeout: TOOLS_TIMEOUT_MS,
		})
		.toBe(true);
}

// Polls, so UI settling after a bridge push stays in the timed window.
export async function runExpect(page: Page, host: HostState, step: ExpectStep): Promise<void> {
	if (step.kind === "expect-host") {
		await expect.poll(() => step.check(host), { message: step.label }).toBeUndefined().catch((error: Error) => {
			throw new Error(`${step.label}: ${error.message}`);
		});
		return;
	}
	await expect
		.poll(async () => valueAtPath(await callTool(page, step.read), step.path), {
			message: `${step.read} ${step.path}`,
		})
		.toEqual(step.equals);
}

// Dot-separated segments: a property, an array index, `*` (map the rest
// over an array), or `[key=value]` (the element whose key equals value).
export function valueAtPath(value: unknown, path: string): unknown {
	return readSegments(value, path === "" ? [] : path.split("."));
}

function readSegments(value: unknown, segments: string[]): unknown {
	if (segments.length === 0 || value === undefined || value === null) return value;
	const [segment, ...rest] = segments;
	if (segment === "*") return Array.isArray(value) ? value.map((item) => readSegments(item, rest)) : undefined;
	const match = /^\[(\w+)=(.*)\]$/.exec(segment);
	if (match) {
		const [, key, expected] = match;
		const found = Array.isArray(value)
			? value.find((item) => String((item as Record<string, unknown>)?.[key]) === expected)
			: undefined;
		return readSegments(found, rest);
	}
	return readSegments((value as Record<string, unknown>)[segment], rest);
}
