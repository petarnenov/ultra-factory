// Benchmark record shared by the e2e bench spec and the dashboard. Keep this
// module free of Node and browser APIs so both sides can import it.

export type Driver = "playwright" | "webmcp-scripted" | "webmcp-jev";

export type BenchStep = {
	index: number;
	tool: string;
	execMs: number;
	roundTripMs: number;
	decisionMs?: number;
	choice?: string;
	confidence?: number;
	probabilities?: Record<string, number>;
	matchedScripted?: boolean;
};

export type BenchRecord = {
	schema: 1;
	runId: string; // unique batch identity; reruns use a new ID
	label: string;
	commit: string;
	timestamp: string;
	env: {
		electron?: string;
		chromium: string;
		node: string;
		platform: string;
		arch: string;
		cpus: number;
		cpuModel: string;
		totalMemoryBytes: number;
		viewport: string;
	};
	scenario: string; // "board.card-ready" | ... | "overhead.evaluate-noop"
	page: "board" | "settings" | "chat" | "overhead";
	driver: Driver;
	repeat: number;
	warmup: boolean;
	pass: boolean;
	error?: string;
	totalMs: number; // timed window, bridge steps excluded
	assertMs: number; // measured assertion time, included in totalMs
	steps: BenchStep[]; // [] for playwright
	jev?: {
		model: string;
		inputTokens: number;
		outputTokens: number;
		attempts: { status: number | null; ms: number }[];
	};
};

export type BenchSummaryRow = {
	scenario: string;
	page: string;
	driver: Driver;
	n: number;
	passRate: number;
	medianMs: number | null;
	minMs: number | null;
	maxMs: number | null;
	ratioToPlaywright: number | null;
	// Medians are over passing measured repeats; matchRate is over the steps
	// that carry a matchedScripted flag. All are null when nothing passed.
	jev?: {
		medianDecisionMs: number | null;
		medianExecMs: number | null;
		medianAssertMs: number | null;
		medianSteps: number | null;
		matchRate: number | null;
	};
};

export const DRIVERS: Driver[] = [
	"playwright",
	"webmcp-scripted",
	"webmcp-jev",
];

const PAGES: BenchRecord["page"][] = ["board", "settings", "chat", "overhead"];

function isObject(v: unknown): v is Record<string, unknown> {
	return typeof v === "object" && v !== null && !Array.isArray(v);
}

function isString(v: unknown): v is string {
	return typeof v === "string";
}

function isNumber(v: unknown): v is number {
	return typeof v === "number" && Number.isFinite(v);
}

function isBoolean(v: unknown): v is boolean {
	return typeof v === "boolean";
}

function isOneOf<T extends string>(v: unknown, options: readonly T[]): boolean {
	return options.some((option) => option === v);
}

// Optional fields may be absent, but must have the right type when present.
function isOptional(v: unknown, check: (x: unknown) => boolean): boolean {
	return v === undefined || check(v);
}

function isStep(v: unknown): boolean {
	return (
		isObject(v) &&
		isNumber(v.index) &&
		isString(v.tool) &&
		isNumber(v.execMs) &&
		isNumber(v.roundTripMs) &&
		isOptional(v.decisionMs, isNumber) &&
		isOptional(v.choice, isString) &&
		isOptional(v.confidence, isNumber) &&
		isOptional(v.probabilities, (probabilities) => isObject(probabilities) && Object.values(probabilities).every(isNumber)) &&
		isOptional(v.matchedScripted, isBoolean)
	);
}

function isAttempt(v: unknown): boolean {
	return (
		isObject(v) &&
		(v.status === null || isNumber(v.status)) &&
		isNumber(v.ms)
	);
}

function isJev(v: unknown): boolean {
	return (
		isObject(v) &&
		isString(v.model) &&
		isNumber(v.inputTokens) &&
		isNumber(v.outputTokens) &&
		Array.isArray(v.attempts) &&
		v.attempts.every(isAttempt)
	);
}

// Throws listing every missing or mistyped field, so a broken record reports
// all of its problems at once instead of the first one.
export function validateRecord(r: unknown): asserts r is BenchRecord {
	if (!isObject(r)) throw new Error("invalid benchmark record: not an object");
	const env = isObject(r.env) ? r.env : {};
	const problems: string[] = [];
	const check = (ok: boolean, field: string) => {
		if (!ok) problems.push(field);
	};
	check(r.schema === 1, "schema");
	check(isString(r.runId), "runId");
	check(isString(r.label), "label");
	check(isString(r.commit), "commit");
	check(isString(r.timestamp), "timestamp");
	check(isObject(r.env), "env");
	check(isString(env.chromium), "env.chromium");
	check(isOptional(env.electron, isString), "env.electron");
	check(isString(env.node), "env.node");
	check(isString(env.platform), "env.platform");
	check(isString(env.arch), "env.arch");
	check(isNumber(env.cpus), "env.cpus");
	check(isString(env.cpuModel), "env.cpuModel");
	check(isNumber(env.totalMemoryBytes), "env.totalMemoryBytes");
	check(isString(env.viewport), "env.viewport");
	check(isString(r.scenario), "scenario");
	check(isOneOf(r.page, PAGES), "page");
	check(isOneOf(r.driver, DRIVERS), "driver");
	check(isNumber(r.repeat), "repeat");
	check(isBoolean(r.warmup), "warmup");
	check(isBoolean(r.pass), "pass");
	check(isOptional(r.error, isString), "error");
	check(isNumber(r.totalMs), "totalMs");
	check(isNumber(r.assertMs), "assertMs");
	check(Array.isArray(r.steps) && r.steps.every(isStep), "steps");
	check(isOptional(r.jev, isJev), "jev");
	if (problems.length > 0) {
		throw new Error(
			`invalid benchmark record: ${problems.join(", ")}`,
		);
	}
}

type Group = {
	scenario: string;
	page: string;
	driver: Driver;
	records: BenchRecord[];
};

function median(values: number[]): number | null {
	if (values.length === 0) return null;
	const sorted = [...values].sort((a, b) => a - b);
	const mid = Math.floor(sorted.length / 2);
	return sorted.length % 2 === 1
		? sorted[mid]
		: (sorted[mid - 1] + sorted[mid]) / 2;
}

// Sum of the defined values, or null when no step carries the field.
function sumField(
	steps: BenchStep[],
	field: "decisionMs" | "execMs",
): number | null {
	const values = steps
		.map((step) => step[field])
		.filter((value): value is number => value !== undefined);
	if (values.length === 0) return null;
	return values.reduce((total, value) => total + value, 0);
}

function jevSummary(
	passed: BenchRecord[],
): NonNullable<BenchSummaryRow["jev"]> {
	const decisions = passed
		.map((r) => sumField(r.steps, "decisionMs"))
		.filter((value): value is number => value !== null);
	const flagged = passed.flatMap((r) =>
		r.steps.filter((step) => step.matchedScripted !== undefined),
	);
	const matched = flagged.filter((step) => step.matchedScripted).length;
	return {
		medianDecisionMs: median(decisions),
		medianExecMs: median(
			passed.map((r) => sumField(r.steps, "execMs") ?? 0),
		),
		medianAssertMs: median(passed.map((r) => r.assertMs)),
		medianSteps: median(passed.map((r) => r.steps.length)),
		matchRate: flagged.length === 0 ? null : matched / flagged.length,
	};
}

function summaryRow(group: Group): BenchSummaryRow {
	const passed = group.records.filter((r) => r.pass);
	const latencies = passed.map((r) => r.totalMs);
	const row: BenchSummaryRow = {
		scenario: group.scenario,
		page: group.page,
		driver: group.driver,
		n: group.records.length,
		passRate: passed.length / group.records.length,
		medianMs: median(latencies),
		minMs: latencies.length > 0 ? Math.min(...latencies) : null,
		maxMs: latencies.length > 0 ? Math.max(...latencies) : null,
		ratioToPlaywright: null,
	};
	if (group.driver === "webmcp-jev") row.jev = jevSummary(passed);
	return row;
}

// Pure and deterministic: input order does not change the output. Warm-up
// repeats are dropped; failed measured repeats count in n and passRate only.
export function summarize(records: BenchRecord[]): BenchSummaryRow[] {
	const groups = new Map<string, Group>();
	for (const r of records) {
		if (r.warmup) continue;
		const key = JSON.stringify([r.scenario, r.page, r.driver]);
		const group = groups.get(key);
		if (group) group.records.push(r);
		else {
			groups.set(key, {
				scenario: r.scenario,
				page: r.page,
				driver: r.driver,
				records: [r],
			});
		}
	}
	const rows = [...groups.values()]
		.sort(
			(a, b) =>
				a.scenario.localeCompare(b.scenario) ||
				a.page.localeCompare(b.page) ||
				DRIVERS.indexOf(a.driver) - DRIVERS.indexOf(b.driver),
		)
		.map(summaryRow);
	const baseline = new Map<string, number | null>();
	for (const row of rows) {
		if (row.driver === "playwright") {
			baseline.set(JSON.stringify([row.scenario, row.page]), row.medianMs);
		}
	}
	return rows.map((row) => {
		const base = baseline.get(JSON.stringify([row.scenario, row.page])) ?? null;
		const ratioToPlaywright =
			row.medianMs === null || base === null || base === 0
				? null
				: row.medianMs / base;
		return { ...row, ratioToPlaywright };
	});
}
