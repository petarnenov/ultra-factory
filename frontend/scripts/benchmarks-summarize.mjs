#!/usr/bin/env node
import { execFileSync } from "node:child_process";
import { mkdir, readFile, readdir, stat, writeFile } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../..");
const defaultOut = path.join(root, "frontend/src/renderer/benchmarks/data");
const dashboardRecords = path.join(defaultOut, "measurements.json");
const dashboardSummary = path.join(defaultOut, "summary.json");
const reportRecords = path.join(root, "docs/performance/webmcp/measurements.json");
const scenarios = [
	"board.columns",
	"board.card-needs-review",
	"board.card-ready",
	"board.new-card",
	"settings.sections",
	"settings.update-downloaded",
	"settings.update-live-flow",
	"chat.suggestion-accept",
	"chat.suggestion-dismiss",
	"chat.retry-refused",
	"overhead.evaluate-noop",
];
const drivers = ["playwright", "webmcp-scripted", "webmcp-jev"];

function usage() {
	console.error(
		"Usage: node frontend/scripts/benchmarks-summarize.mjs <dir-or-json> [--out DIR] [--check] [--secret-scan]",
	);
	process.exit(2);
}

const args = process.argv.slice(2);
const input = args.shift();
if (!input) usage();
let out = defaultOut;
let checkOnly = false;
let scanSecrets = false;
while (args.length > 0) {
	const arg = args.shift();
	if (arg === "--out") {
		out = path.resolve(args.shift() ?? usage());
	} else if (arg === "--check") {
		checkOnly = true;
	} else if (arg === "--secret-scan") {
		scanSecrets = true;
	} else {
		usage();
	}
}

const { summarize, validateRecord } = await import(
	pathToFileURL(path.join(root, "frontend/src/renderer/benchmarks/record.ts"))
);

async function parseRecords(file) {
	const value = JSON.parse(await readFile(file, "utf8"));
	const records = Array.isArray(value) ? value : [value];
	for (const record of records) validateRecord(record);
	return records;
}

async function readInput(source) {
	const resolved = path.resolve(source);
	if ((await stat(resolved)).isDirectory()) {
		const files = (await readdir(resolved))
			.filter((file) => file.endsWith(".json"))
			.sort();
		const records = [];
		for (const file of files) records.push(...(await parseRecords(path.join(resolved, file))));
		return records;
	}
	return parseRecords(resolved);
}

function compare(a, b) {
	return a < b ? -1 : a > b ? 1 : 0;
}

function sortRecords(records) {
	return [...records].sort(
		(a, b) =>
			compare(a.scenario, b.scenario) ||
			compare(a.driver, b.driver) ||
			compare(a.runId, b.runId) ||
			a.repeat - b.repeat,
	);
}

function checkDuplicates(records) {
	const seen = new Set();
	for (const record of records) {
		const identity = JSON.stringify([
			record.runId,
			record.scenario,
			record.driver,
			record.repeat,
		]);
		if (seen.has(identity)) throw new Error("duplicate run/scenario/driver/repeat identity");
		seen.add(identity);
	}
}

function checkCoverage(records) {
	const problems = [];
	for (const scenario of scenarios) {
		for (const driver of drivers) {
			const count = records.filter(
				(record) => record.scenario === scenario && record.driver === driver && !record.warmup,
			).length;
			if (count < 5) problems.push(`${scenario} ${driver}: ${count}/5 measured attempts`);
		}
	}
	const unexpected = new Set(records.map((record) => record.scenario));
	for (const scenario of unexpected) {
		if (!scenarios.includes(scenario)) problems.push(`unexpected scenario: ${scenario}`);
	}
	if (problems.length > 0) throw new Error(`coverage check failed:\n${problems.join("\n")}`);
}

function summaryBytes(records) {
	return `${JSON.stringify(summarize(records), null, 2)}\n`;
}

function reportTable(rows) {
	const formatMs = (value) => value === null ? "—" : `${value.toFixed(1)} ms`;
	const lines = [
		"| Scenario | Driver | Median (min–max) | Pass rate |",
		"| --- | --- | ---: | ---: |",
	];
	for (const row of rows) {
		const range = row.medianMs === null
			? "—"
			: `${formatMs(row.medianMs)} (${formatMs(row.minMs)}–${formatMs(row.maxMs)})`;
		lines.push(
			`| ${row.scenario} | ${row.driver} | ${range} | ${(row.passRate * 100).toFixed(0)}% (${row.n}) |`,
		);
	}
	return lines.join("\n");
}

async function secretScan(records) {
	const secret = process.env.TYPESAFE_API_KEY || process.env.JEV_MAF_LAB;
	if (!secret) throw new Error("secret scan requested but no benchmark key is configured");
	const needle = Buffer.from(secret);
	const tracked = execFileSync("git", ["ls-files", "-z"], { cwd: root });
	const files = tracked.toString("utf8").split("\0").filter(Boolean);
	for (const file of files) {
		try {
			if ((await readFile(path.join(root, file))).includes(needle)) {
				throw new Error(`configured key found in tracked file: ${file}`);
			}
		} catch (error) {
			if (error.code !== "ENOENT") throw error;
		}
	}
	for (const file of [dashboardRecords, reportRecords]) {
		try {
			if ((await readFile(file)).includes(needle)) {
				throw new Error(`configured key found in benchmark records: ${path.relative(root, file)}`);
			}
		} catch (error) {
			if (error.code !== "ENOENT") throw error;
		}
	}
	if (Buffer.from(JSON.stringify(records)).includes(needle)) {
		throw new Error("configured key found in input records");
	}
	console.log("Secret scan passed.");
}

const records = sortRecords(await readInput(input));
checkDuplicates(records);
const summary = summaryBytes(records);

if (checkOnly) {
	checkCoverage(records);
	const dashboard = await readFile(dashboardRecords, "utf8");
	const report = await readFile(reportRecords, "utf8");
	if (dashboard !== report) throw new Error("dashboard and report measurement copies differ");
	if (dashboard !== `${JSON.stringify(records, null, 2)}\n`) {
		throw new Error("input measurements do not match the committed measurement copies");
	}
	const committedSummary = await readFile(dashboardSummary, "utf8");
	if (committedSummary !== summary) throw new Error("summary.json is not reproducible from measurements.json");
	if (scanSecrets) await secretScan(records);
	console.log(`Validated ${records.length} records, coverage, duplicate identities, copies, and summary.`);
} else {
	await mkdir(out, { recursive: true });
	await writeFile(path.join(out, "measurements.json"), `${JSON.stringify(records, null, 2)}\n`);
	await writeFile(path.join(out, "summary.json"), summary);
	if (scanSecrets) await secretScan(records);
	console.log(reportTable(summarize(records)));
}
