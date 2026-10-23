import { expect, test, type Page } from "@playwright/test";
import { playwright } from "./webmcp/drivers/playwright";
import type { BenchScenario } from "./webmcp/drivers/types";
import { createHostState, PlaywrightClock, scenarios, type BridgeStep } from "./webmcp/scenarios";

const bridgeMs = 60;
const assertMs = 30;
const wait = (ms: number) => new Promise<void>((resolve) => setTimeout(resolve, ms));
const slowBridge: BridgeStep = { kind: "bridge", label: "slow bridge", run: () => wait(bridgeMs) };

// Bridge, then a passing and a failing assertion, as a late scenario failure.
const failingAfterBridge: BenchScenario = {
	...scenarios[0],
	playwrightReady: async () => {},
	playwright: async (page) => {
		const clock = new PlaywrightClock();
		await clock.bridge(page, slowBridge);
		await clock.assert(() => wait(assertMs));
		await clock.assert(async () => {
			await wait(assertMs);
			throw new Error("last assertion failed");
		});
		return clock.timing();
	},
};

test("the playwright driver keeps the scenario clock when the body fails", async () => {
	const startedAt = performance.now();
	const result = await playwright({} as Page, createHostState(), failingAfterBridge);
	const wallMs = performance.now() - startedAt;

	expect(result.pass).toBe(false);
	expect(result.error).toBe("last assertion failed");
	expect(result.assertMs).toBeGreaterThanOrEqual(2 * assertMs - 5);
	expect(result.totalMs).toBeGreaterThanOrEqual(result.assertMs);
	expect(result.totalMs).toBeLessThanOrEqual(wallMs - bridgeMs + 5);
});
