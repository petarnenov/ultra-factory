import { errorMessage, type Driver } from "./types";

export const playwright: Driver = async (page, host, s) => {
	await s.playwrightReady(page, host);
	const startedAt = performance.now();
	try {
		const { bridgeMs, assertMs } = await s.playwright(page, host);
		return { pass: true, totalMs: performance.now() - startedAt - bridgeMs, assertMs, steps: [] };
	} catch (error) {
		// The scenario clock is lost on a throw, so a failed run's totalMs still
		// includes bridge time and its assertMs is unknown (0).
		return { pass: false, totalMs: performance.now() - startedAt, assertMs: 0, steps: [], error: errorMessage(error) };
	}
};
