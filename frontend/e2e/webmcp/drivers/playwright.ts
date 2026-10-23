import { runPlaywrightBody } from "../scenarios";
import { errorMessage, type Driver } from "./types";

export const playwright: Driver = async (page, host, s) => {
	await s.playwrightReady(page, host);
	const startedAt = performance.now();
	const { timing, error } = await runPlaywrightBody(() => s.playwright(page, host));
	// Without a clock there is no bridge or assertion split; keep wall time.
	const { totalMs, assertMs } = timing ?? { totalMs: performance.now() - startedAt, assertMs: 0 };
	const failure = error === undefined ? {} : { error: errorMessage(error) };
	return { pass: error === undefined, totalMs, assertMs, steps: [], ...failure };
};
