import { useMemo } from "react";
import measurements from "./data/measurements.json";
import suppliedSummary from "./data/summary.json";
import { summarize, validateRecord, type BenchRecord, type BenchSummaryRow } from "./record";

export type BenchmarkData = {
	records: BenchRecord[];
	summary: BenchSummaryRow[];
};

export function validateBenchmarkData(recordsValue: unknown, summaryValue: unknown): BenchmarkData {
	if (!Array.isArray(recordsValue)) throw new Error("benchmark measurements must be an array");
	if (!Array.isArray(summaryValue)) throw new Error("benchmark summary must be an array");
	recordsValue.forEach(validateRecord);
	const records = recordsValue as BenchRecord[];
	const summary = summaryValue as BenchSummaryRow[];
	const order = (row: BenchSummaryRow) => `${row.scenario}\u0000${row.page}\u0000${row.driver}`;
	const expected = summarize(records).sort((left, right) => order(left).localeCompare(order(right)));
	const supplied = [...summary].sort((left, right) => order(left).localeCompare(order(right)));
	if (JSON.stringify(expected) !== JSON.stringify(supplied)) {
		throw new Error("benchmark summary does not match measurements");
	}
	return { records, summary };
}

export function loadBenchmarkData(): BenchmarkData {
	return validateBenchmarkData(measurements, suppliedSummary);
}

export function useBenchmarkData() {
	return useMemo(() => {
		try {
			return { ...loadBenchmarkData(), error: null };
		} catch (error) {
			return {
				records: [] as BenchRecord[],
				summary: [] as BenchSummaryRow[],
				error: error instanceof Error ? error : new Error("benchmark data is unavailable"),
			};
		}
	}, []);
}
