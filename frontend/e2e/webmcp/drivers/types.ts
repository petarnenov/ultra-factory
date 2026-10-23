import type { Page } from "@playwright/test";
import type { BenchRecord, BenchStep } from "../../../src/renderer/benchmarks/record";
import type { HostState, Scenario } from "../scenarios";

export type StepTiming = BenchStep;

// A failed run keeps its elapsed time and completed steps; never zeroed.
export type DriverResult = { pass: boolean; totalMs: number; assertMs: number; steps: StepTiming[]; error?: string; jev?: BenchRecord["jev"] };

// A catalog scenario, or a bench-only series such as the overhead one.
export type BenchScenario = Omit<Scenario, "page"> & { page: BenchRecord["page"] };

// Setup and readiness failures throw; failures in the timed window resolve.
export type Driver = (page: Page, host: HostState, s: BenchScenario) => Promise<DriverResult>;

const ansiEscape = /\u001b\[[0-9;]*m/g;

// Plain text: records are JSON evidence, not terminal output.
export function errorMessage(error: unknown): string {
	return (error instanceof Error ? error.message : String(error)).replace(ansiEscape, "");
}
