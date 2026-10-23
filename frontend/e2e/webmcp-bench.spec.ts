/// <reference path="../src/renderer/webmcp/global.d.ts" />
import { expect, test, type Browser, type Page } from "@playwright/test";
import { execFileSync } from "node:child_process";
import { randomUUID } from "node:crypto";
import { mkdir, writeFile } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import {
	validateRecord,
	type BenchRecord,
	type Driver as DriverName,
} from "../src/renderer/benchmarks/record";
import { installFakeAgent } from "./support/fake-bridge";
import { waitForTools } from "./webmcp/assert";
import { playwright } from "./webmcp/drivers/playwright";
import { scripted } from "./webmcp/drivers/scripted";
import type { BenchScenario, Driver, DriverResult } from "./webmcp/drivers/types";
import { createHostState, scenarios, type PhaseAction } from "./webmcp/scenarios";

// Set in the runner while it loads this file, so every worker it spawns
// (including restarts after a failure) shares one batch identity.
process.env.AO_WEBMCP_RUN_ID ??= randomUUID();
const runId = process.env.AO_WEBMCP_RUN_ID;
const label = process.env.AO_PERF_LABEL ?? "sample";
const repeats = Number(process.env.AO_WEBMCP_REPEATS ?? 6);

// ── Overhead series ──────────────────────────────────────────────────────────

const noopTool = "overhead.noop";
const noopCalls = 50;

// Test-only: registered once the app installs its shim, never in production.
async function installNoopTool(page: Page): Promise<void> {
	await page.addInitScript((name) => {
		const timer = setInterval(() => {
			if (!document.modelContext || !window.__aoWebMcp) return;
			clearInterval(timer);
			document.modelContext.registerTool({
				name,
				description: "Do nothing and return null.",
				inputSchema: { type: "object" },
				execute: () => null,
			});
		}, 10);
	}, noopTool);
}

async function removeNoopTool(page: Page): Promise<void> {
	await page.evaluate((name) => document.modelContext?.unregisterTool(name), noopTool);
}

const noopActions: PhaseAction[] = Array.from({ length: noopCalls }, (_, i) => ({
	kind: "tool",
	id: `noop-${i}`,
	name: noopTool,
}));

const overheadScenario: BenchScenario = {
	id: "overhead.evaluate-noop",
	page: "overhead",
	route: "/#/",
	source: { file: "e2e/webmcp-bench.spec.ts", title: "overhead series" },
	setup: async (page) => {
		await installFakeAgent(page);
		await installNoopTool(page);
	},
	phases: [{ id: "noop", actions: noopActions, checks: [] }],
	tools: noopActions,
	playwrightReady: (page) => waitForTools(page, noopTool),
	playwright: async (page) => {
		const startedAt = performance.now();
		for (let i = 0; i < noopCalls; i++) await page.evaluate(() => null);
		return { totalMs: performance.now() - startedAt, bridgeMs: 0, assertMs: 0 };
	},
	jevActions: noopActions.map((step) => ({ id: step.id, phase: "noop", tool: step.name, args: {} })),
	jevGoal: `Call ${noopTool} ${noopCalls} times.`,
};

// ── Records ──────────────────────────────────────────────────────────────────

const commit = execFileSync("git", ["rev-parse", "HEAD"], { encoding: "utf8" }).trim();

type RunContext = {
	scenario: BenchScenario;
	driver: DriverName;
	repeat: number;
	timestamp: string;
	browser: Browser;
	page: Page;
};

function buildRecord(run: RunContext, result: DriverResult): BenchRecord {
	const viewport = run.page.viewportSize();
	return {
		schema: 1,
		runId,
		label,
		commit,
		timestamp: run.timestamp,
		env: {
			chromium: run.browser.version(),
			node: process.version,
			platform: os.platform(),
			arch: os.arch(),
			cpus: os.cpus().length,
			cpuModel: os.cpus()[0]?.model ?? "unknown",
			totalMemoryBytes: os.totalmem(),
			viewport: viewport ? `${viewport.width}x${viewport.height}` : "unknown",
		},
		scenario: run.scenario.id,
		page: run.scenario.page,
		driver: run.driver,
		repeat: run.repeat,
		warmup: run.repeat === 0,
		...result,
	};
}

async function writeRecord(record: BenchRecord): Promise<void> {
	const output = process.env.AO_PERF_DIR;
	if (!output) return;
	await mkdir(output, { recursive: true });
	const file = `${label}-${record.scenario}-${record.driver}-${record.repeat}.json`;
	await writeFile(path.join(output, file), JSON.stringify(record, null, 2) + "\n");
}

// ── Bench ────────────────────────────────────────────────────────────────────

const drivers: Partial<Record<DriverName, Driver>> = { playwright, "webmcp-scripted": scripted };

// Still run and recorded as failures; the annotation names the cause.
const knownFailures: Record<string, string> = {
	"settings.update-downloaded · playwright":
		"Copies updates-settings.spec.ts test 1, which fails on dev: stale status-line copy",
};

test.describe("WebMCP benchmark", () => {
	test.skip(process.env.AO_WEBMCP_BENCH !== "1", "Opt-in benchmark; set AO_WEBMCP_BENCH=1");
	// Independent tests, not a serial group: a failure must not skip the rest.
	test.describe.configure({ mode: "default", retries: 0 });

	for (const scenario of [...scenarios, overheadScenario]) {
		for (const [driver, run] of Object.entries(drivers) as [DriverName, Driver][]) {
			for (let repeat = 0; repeat < repeats; repeat++) {
				test(`${scenario.id} · ${driver} · ${repeat}`, async ({ page, browser }, testInfo) => {
					test.setTimeout(120_000);
					const knownFailure = knownFailures[`${scenario.id} · ${driver}`];
					if (knownFailure) testInfo.annotations.push({ type: "known-failure", description: knownFailure });
					const timestamp = new Date().toISOString();
					const host = createHostState();
					await scenario.setup(page, host);
					await page.goto(scenario.route);
					const result = await run(page, host, scenario);
					if (scenario === overheadScenario) await removeNoopTool(page);
					const record = buildRecord({ scenario, driver, repeat, timestamp, browser, page }, result);
					validateRecord(record);
					await testInfo.attach("record", { body: JSON.stringify(record, null, 2), contentType: "application/json" });
					await writeRecord(record);
					expect(result.pass, result.error).toBe(true);
				});
			}
		}
	}
});
