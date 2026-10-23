import { describe, expect, it } from "vitest";
import { aggregateDriverPassRates, filterBenchmarkData, getOverheadReference } from "./aggregate";
import { loadBenchmarkData, validateBenchmarkData } from "./load";
import { DRIVERS } from "./record";

const { records, summary } = loadBenchmarkData();

describe("benchmark data loader", () => {
	it("loads the committed measurements and validates their summary", () => {
		expect(summary.length).toBeGreaterThan(0);
		expect(() => validateBenchmarkData(records, [...summary].reverse())).not.toThrow();
		const reordered = summary.map((row) => ({
			...Object.fromEntries(Object.entries(row).reverse()),
			...(row.jev ? { jev: Object.fromEntries(Object.entries(row.jev).reverse()) } : {}),
		}) as typeof row);
		expect(() => validateBenchmarkData(records, reordered)).not.toThrow();
		const changed = [...summary];
		changed[0] = { ...changed[0]!, n: changed[0]!.n + 1 };
		expect(() => validateBenchmarkData(records, changed)).toThrow(/does not match/);
	});

	it("rejects malformed records instead of rendering partial metrics", () => {
		expect(() => validateBenchmarkData([{ driver: "unknown" }], [])).toThrow(/invalid benchmark record/);
	});
});

describe("benchmark aggregates", () => {
	it("filters summary rows and repeat records with the same page and drivers", () => {
		const filtered = filterBenchmarkData(records, summary, "board", new Set(["playwright", "webmcp-jev"]));
		expect(filtered.summary).toEqual(summary.filter((row) => row.page === "board" && ["playwright", "webmcp-jev"].includes(row.driver)));
		expect(filtered.records).toEqual(records.filter((record) => !record.warmup && record.page === "board" && ["playwright", "webmcp-jev"].includes(record.driver)));
	});

	it("weights pass rate by attempts instead of averaging scenario rates", () => {
		const rows = summary.filter((row) => row.driver === "webmcp-jev");
		const rate = aggregateDriverPassRates(rows).find((row) => row.driver === "webmcp-jev");
		const attempts = rows.reduce((total, row) => total + row.n, 0);
		const passes = rows.reduce((total, row) => total + Math.round(row.passRate * row.n), 0);
		expect(rate).toMatchObject({ attempts, passes, passRate: passes / attempts });
	});

	it("keeps overhead calibration available when a page filter is active", () => {
		const board = filterBenchmarkData(records, summary, "board", new Set(DRIVERS));
		expect(getOverheadReference(records)).toEqual(expect.any(Number));
		expect(getOverheadReference(records, board.records)).toEqual(expect.any(Number));
		expect(aggregateDriverPassRates(summary).map((row) => row.driver)).toEqual(DRIVERS);
	});
});
