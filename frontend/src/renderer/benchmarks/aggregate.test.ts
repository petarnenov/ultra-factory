import { describe, expect, it } from "vitest";
import { aggregateDriverPassRates, filterBenchmarkData, getOverheadReference } from "./aggregate";
import { loadBenchmarkData, validateBenchmarkData } from "./load";
import { DRIVERS } from "./record";

const { records, summary } = loadBenchmarkData();

describe("benchmark data loader", () => {
	it("loads the committed fixture and validates its summary against raw records", () => {
		expect(summary).toHaveLength(11);
		expect(() => validateBenchmarkData(records, [...summary].reverse())).not.toThrow();
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
		expect(filtered.summary.map((row) => row.driver)).toEqual(["playwright", "webmcp-jev"]);
		expect(filtered.records).toHaveLength(2);
	});

	it("weights pass rate by attempts instead of averaging scenario rates", () => {
		const rows = summary.filter((row) => row.driver === "webmcp-jev");
		const rate = aggregateDriverPassRates(rows).find((row) => row.driver === "webmcp-jev");
		expect(rate).toMatchObject({ attempts: 4, passes: 3, passRate: 0.75 });
	});

	it("uses measured browser call overhead and measured WebMCP step counts", () => {
		expect(getOverheadReference(records)).toBe(12.5);
		expect(aggregateDriverPassRates(summary).map((row) => row.driver)).toEqual(DRIVERS);
	});
});
