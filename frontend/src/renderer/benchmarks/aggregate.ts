import { DRIVERS, type BenchRecord, type BenchSummaryRow, type Driver } from "./record";

export type BenchmarkPageFilter = "all" | BenchRecord["page"];

export function filterBenchmarkData(
	records: BenchRecord[],
	summary: BenchSummaryRow[],
	page: BenchmarkPageFilter,
	drivers: ReadonlySet<Driver>,
) {
	const matchesPage = (value: string) => page === "all" || value === page;
	return {
		records: records.filter((record) => !record.warmup && matchesPage(record.page) && drivers.has(record.driver)),
		summary: summary.filter((row) => matchesPage(row.page) && drivers.has(row.driver)),
	};
}

export function median(values: number[]): number | null {
	if (values.length === 0) return null;
	const sorted = [...values].sort((a, b) => a - b);
	const middle = Math.floor(sorted.length / 2);
	return sorted.length % 2 ? sorted[middle] : (sorted[middle - 1] + sorted[middle]) / 2;
}

export function aggregateDriverPassRates(summary: BenchSummaryRow[]) {
	return DRIVERS.map((driver) => {
		const rows = summary.filter((row) => row.driver === driver);
		const attempts = rows.reduce((total, row) => total + row.n, 0);
		const passes = rows.reduce((total, row) => total + Math.round(row.passRate * row.n), 0);
		return { driver, attempts, passes, passRate: attempts === 0 ? null : passes / attempts };
	});
}

export function getOverheadReference(
	calibrationRecords: BenchRecord[],
	flowRecords: BenchRecord[] = calibrationRecords,
): number | null {
	const callOverheads = calibrationRecords
		.filter((record) => !record.warmup && record.page === "overhead" && record.pass && record.driver !== "playwright")
		.flatMap((record) => record.steps.map((step) => step.roundTripMs));
	const measuredFlowStepCounts = flowRecords
		.filter((record) => !record.warmup && record.page !== "overhead" && record.pass && record.driver !== "playwright")
		.map((record) => record.steps.length);
	const overheadPerCall = median(callOverheads);
	const medianCallsPerFlow = median(measuredFlowStepCounts);
	return overheadPerCall === null || medianCallsPerFlow === null
		? null
		: overheadPerCall * medianCallsPerFlow;
}

export function driverOrder(driver: Driver): number {
	return DRIVERS.indexOf(driver);
}
