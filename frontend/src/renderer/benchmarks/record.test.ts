import { describe, expect, it } from "vitest";
import {
	type BenchRecord,
	type BenchSummaryRow,
	DRIVERS,
	summarize,
	validateRecord,
} from "./record";

const base: BenchRecord = {
	schema: 1,
	runId: "run-1",
	label: "wm-3.1.1",
	commit: "abc123",
	timestamp: "2026-10-22T00:00:00.000Z",
	env: {
		chromium: "130.0.6723.0",
		node: "v22.0.0",
		platform: "linux",
		arch: "x64",
		cpus: 8,
		cpuModel: "test cpu",
		totalMemoryBytes: 16_000_000_000,
		viewport: "1440x1000",
	},
	scenario: "board.card-ready",
	page: "board",
	driver: "playwright",
	repeat: 1,
	warmup: false,
	pass: true,
	totalMs: 100,
	assertMs: 5,
	steps: [],
};

function rec(overrides: Partial<BenchRecord>): BenchRecord {
	return { ...base, ...overrides };
}

function withoutField(
	record: BenchRecord,
	field: keyof BenchRecord,
): Record<string, unknown> {
	const copy: Record<string, unknown> = { ...record };
	delete copy[field];
	return copy;
}

const jevRecord = rec({
	driver: "webmcp-jev",
	repeat: 1,
	totalMs: 300,
	assertMs: 4,
	steps: [
		{
			index: 0,
			tool: "board.open",
			execMs: 2,
			roundTripMs: 10,
			decisionMs: 150,
			choice: "open",
			confidence: 0.9,
			matchedScripted: true,
		},
		{
			index: 1,
			tool: "done",
			execMs: 1,
			roundTripMs: 5,
			decisionMs: 220,
			matchedScripted: false,
		},
	],
	jev: {
		model: "jev-test",
		inputTokens: 373,
		outputTokens: 43,
		attempts: [{ status: 200, ms: 151 }],
	},
});

// Fixed fixture: every group and edge case the summary contract covers.
const fixture: BenchRecord[] = [
	rec({ repeat: 0, warmup: true, totalMs: 999 }),
	...[100, 120, 140, 160, 180].map((totalMs, i) =>
		rec({ repeat: i + 1, totalMs }),
	),
	rec({ driver: "webmcp-scripted", repeat: 1, totalMs: 150 }),
	rec({ driver: "webmcp-scripted", repeat: 2, totalMs: 170 }),
	rec({ driver: "webmcp-scripted", repeat: 3, totalMs: 190 }),
	rec({
		driver: "webmcp-scripted",
		repeat: 4,
		pass: false,
		totalMs: 9000,
		error: "timeout",
	}),
	jevRecord,
	rec({
		driver: "webmcp-jev",
		repeat: 2,
		pass: false,
		totalMs: 2000,
		error: "HTTP 429",
		steps: [
			{ index: 0, tool: "board.open", execMs: 2, roundTripMs: 10 },
		],
		jev: {
			model: "jev-test",
			inputTokens: 0,
			outputTokens: 0,
			attempts: [
				{ status: 429, ms: 40 },
				{ status: null, ms: 2000 },
			],
		},
	}),
	rec({ scenario: "settings.panel", page: "settings", totalMs: 0 }),
	rec({
		scenario: "settings.panel",
		page: "settings",
		driver: "webmcp-scripted",
		totalMs: 50,
	}),
	rec({
		scenario: "settings.panel",
		page: "settings",
		driver: "webmcp-jev",
		repeat: 1,
		pass: false,
		totalMs: 500,
		error: "no response",
	}),
	rec({
		scenario: "settings.panel",
		page: "settings",
		driver: "webmcp-jev",
		repeat: 2,
		pass: false,
		totalMs: 500,
		error: "no response",
	}),
];

describe("validateRecord", () => {
	it("accepts a valid record", () => {
		expect(() => validateRecord(base)).not.toThrow();
		expect(() => validateRecord(jevRecord)).not.toThrow();
	});

	it("rejects a record without driver", () => {
		const invalid = withoutField(base, "driver");
		expect(() => validateRecord(invalid)).toThrow(/driver/);
	});

	it("rejects a record without env.chromium", () => {
		const env: Record<string, unknown> = { ...base.env };
		delete env.chromium;
		expect(() => validateRecord({ ...base, env })).toThrow(/env\.chromium/);
	});

	it("rejects a record without steps", () => {
		const invalid = withoutField(base, "steps");
		expect(() => validateRecord(invalid)).toThrow(/steps/);
	});

	it("lists every missing field in one error", () => {
		expect(() => validateRecord({})).toThrow(
			/schema, runId, label.*driver.*steps/,
		);
	});
});

describe("DRIVERS", () => {
	it("lists the three drivers in report order", () => {
		expect(DRIVERS).toEqual(["playwright", "webmcp-scripted", "webmcp-jev"]);
	});
});

describe("summarize", () => {
	const rows = summarize(fixture);
	const find = (scenario: string, driver: string): BenchSummaryRow => {
		const row = rows.find(
			(r) => r.scenario === scenario && r.driver === driver,
		);
		if (!row) throw new Error(`missing row ${scenario} ${driver}`);
		return row;
	};

	it("drops warm-up repeats", () => {
		const playwright = find("board.card-ready", "playwright");
		expect(playwright.n).toBe(5);
		expect(playwright.maxMs).toBe(180);
	});

	it("counts failed repeats in n and passRate but not in medians", () => {
		const scripted = find("board.card-ready", "webmcp-scripted");
		expect(scripted.n).toBe(4);
		expect(scripted.passRate).toBe(0.75);
		expect(scripted.medianMs).toBe(170);
		expect(scripted.minMs).toBe(150);
		expect(scripted.maxMs).toBe(190);
	});

	it("computes ratioToPlaywright from the playwright median", () => {
		expect(find("board.card-ready", "playwright").ratioToPlaywright).toBe(1);
		expect(
			find("board.card-ready", "webmcp-scripted").ratioToPlaywright,
		).toBeCloseTo(170 / 140);
	});

	it("reports null latency and ratios when nothing passed", () => {
		const jev = find("settings.panel", "webmcp-jev");
		expect(jev.n).toBe(2);
		expect(jev.passRate).toBe(0);
		expect(jev.medianMs).toBeNull();
		expect(jev.minMs).toBeNull();
		expect(jev.maxMs).toBeNull();
		expect(jev.ratioToPlaywright).toBeNull();
		expect(jev.jev).toEqual({
			medianDecisionMs: null,
			medianExecMs: null,
			medianAssertMs: null,
			medianSteps: null,
			matchRate: null,
		});
	});

	it("reports null ratios when the playwright baseline is zero", () => {
		const scripted = find("settings.panel", "webmcp-scripted");
		expect(scripted.medianMs).toBe(50);
		expect(scripted.ratioToPlaywright).toBeNull();
	});

	it("summarizes JEV decisions, execution, assertion, and matches", () => {
		const jev = find("board.card-ready", "webmcp-jev");
		expect(jev.n).toBe(2);
		expect(jev.passRate).toBe(0.5);
		expect(jev.jev).toEqual({
			medianDecisionMs: 370,
			medianExecMs: 3,
			medianAssertMs: 4,
			medianSteps: 2,
			matchRate: 0.5,
		});
	});

	it("is deterministic regardless of input order", () => {
		expect(summarize([...fixture].reverse())).toEqual(rows);
	});

	it("matches the snapshot for the fixed fixture", () => {
		expect(rows).toMatchInlineSnapshot(`
			[
			  {
			    "driver": "playwright",
			    "maxMs": 180,
			    "medianMs": 140,
			    "minMs": 100,
			    "n": 5,
			    "page": "board",
			    "passRate": 1,
			    "ratioToPlaywright": 1,
			    "scenario": "board.card-ready",
			  },
			  {
			    "driver": "webmcp-scripted",
			    "maxMs": 190,
			    "medianMs": 170,
			    "minMs": 150,
			    "n": 4,
			    "page": "board",
			    "passRate": 0.75,
			    "ratioToPlaywright": 1.2142857142857142,
			    "scenario": "board.card-ready",
			  },
			  {
			    "driver": "webmcp-jev",
			    "jev": {
			      "matchRate": 0.5,
			      "medianAssertMs": 4,
			      "medianDecisionMs": 370,
			      "medianExecMs": 3,
			      "medianSteps": 2,
			    },
			    "maxMs": 300,
			    "medianMs": 300,
			    "minMs": 300,
			    "n": 2,
			    "page": "board",
			    "passRate": 0.5,
			    "ratioToPlaywright": 2.142857142857143,
			    "scenario": "board.card-ready",
			  },
			  {
			    "driver": "playwright",
			    "maxMs": 0,
			    "medianMs": 0,
			    "minMs": 0,
			    "n": 1,
			    "page": "settings",
			    "passRate": 1,
			    "ratioToPlaywright": null,
			    "scenario": "settings.panel",
			  },
			  {
			    "driver": "webmcp-scripted",
			    "maxMs": 50,
			    "medianMs": 50,
			    "minMs": 50,
			    "n": 1,
			    "page": "settings",
			    "passRate": 1,
			    "ratioToPlaywright": null,
			    "scenario": "settings.panel",
			  },
			  {
			    "driver": "webmcp-jev",
			    "jev": {
			      "matchRate": null,
			      "medianAssertMs": null,
			      "medianDecisionMs": null,
			      "medianExecMs": null,
			      "medianSteps": null,
			    },
			    "maxMs": null,
			    "medianMs": null,
			    "minMs": null,
			    "n": 2,
			    "page": "settings",
			    "passRate": 0,
			    "ratioToPlaywright": null,
			    "scenario": "settings.panel",
			  },
			]
		`);
	});
});
