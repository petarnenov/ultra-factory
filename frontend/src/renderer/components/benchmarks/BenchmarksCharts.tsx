import {
	Bar,
	BarChart,
	CartesianGrid,
	Cell,
	ErrorBar,
	LabelList,
	ReferenceLine,
	Scatter,
	ScatterChart,
	Tooltip as RechartsTooltip,
	XAxis,
	YAxis,
} from "recharts";
import type { TFunction } from "i18next";
import { useTranslation } from "react-i18next";
import { aggregateDriverPassRates, getOverheadReference } from "../../benchmarks/aggregate";
import type { BenchRecord, BenchSummaryRow, Driver } from "../../benchmarks/record";
import { ChartContainer, ChartLegend, ChartLegendContent, ChartTooltipContent, type ChartConfig } from "../ui/chart";
import { TableTwin } from "./TableTwin";

const colors: Record<Driver, string> = {
	playwright: "var(--chart-1)",
	"webmcp-scripted": "var(--chart-3)",
	"webmcp-jev": "var(--chart-5)",
};

function driverKey(driver: Driver): string {
	return driver === "playwright" ? "playwright" : driver === "webmcp-scripted" ? "scripted" : "jev";
}

const driverLabelKeys = {
	playwright: "benchmarks.driver.playwright",
	"webmcp-scripted": "benchmarks.driver.webmcpScripted",
	"webmcp-jev": "benchmarks.driver.webmcpJev",
} as const;

function driverLabel(driver: Driver, t: TFunction): string {
	return t(driverLabelKeys[driver]);
}

function formatMs(value: number | null | undefined): string {
	return value == null ? "—" : `${Math.round(value)} ms`;
}

function errorRange(row: BenchSummaryRow | undefined): [number, number] | undefined {
	if (!row || row.medianMs === null || row.minMs === null || row.maxMs === null) return undefined;
	return [row.medianMs - row.minMs, row.maxMs - row.medianMs];
}

function scenariosFrom(summary: BenchSummaryRow[]) {
	const map = new Map<string, { scenario: string; values: Partial<Record<Driver, BenchSummaryRow>> }>();
	for (const row of summary) {
		const entry = map.get(row.scenario) ?? { scenario: row.scenario, values: {} };
		entry.values[row.driver] = row;
		map.set(row.scenario, entry);
	}
	return [...map.values()];
}

function totalsConfig(t: TFunction): ChartConfig {
	return {
		playwright: { label: t("benchmarks.driver.playwright"), color: colors.playwright },
		scripted: { label: t("benchmarks.driver.webmcpScripted"), color: colors["webmcp-scripted"] },
		jev: { label: t("benchmarks.driver.webmcpJev"), color: colors["webmcp-jev"] },
	};
}

function TotalsChart({ summary, records, drivers }: { summary: BenchSummaryRow[]; records: BenchRecord[]; drivers: Driver[] }) {
	const { t } = useTranslation();
	const scenarios = scenariosFrom(summary);
	const data = scenarios.map(({ scenario, values }) => ({
		scenario,
		playwright: values.playwright?.medianMs ?? undefined,
		playwrightError: errorRange(values.playwright),
		scripted: values["webmcp-scripted"]?.medianMs ?? undefined,
		scriptedError: errorRange(values["webmcp-scripted"]),
		jev: values["webmcp-jev"]?.medianMs ?? undefined,
		jevError: errorRange(values["webmcp-jev"]),
	}));
	const overhead = getOverheadReference(records);
	const config = totalsConfig(t);
	const headers = [t("benchmarks.col.scenario"), ...drivers.map((driver) => driverLabel(driver, t))];
	return (
		<div>
			<ChartContainer config={config} className="h-[250px] w-full aspect-auto overflow-hidden">
				<BarChart data={data} margin={{ top: 12, right: 8, bottom: 4, left: 0 }}>
					<CartesianGrid vertical={false} stroke="var(--border)" />
					<XAxis dataKey="scenario" axisLine={false} tickLine={false} tick={{ fontSize: 10 }} interval={0} />
					<YAxis axisLine={false} tickLine={false} width={42} tick={{ fontSize: 10 }} />
					<RechartsTooltip content={<ChartTooltipContent />} />
					<ChartLegend content={<ChartLegendContent />} />
					{overhead !== null && <ReferenceLine y={overhead} stroke="var(--chart-4)" strokeDasharray="4 3" label={{ value: t("benchmarks.overheadReference"), position: "insideTopRight", fill: "var(--muted-foreground)", fontSize: 10 }} />}
					{drivers.includes("playwright") && <Bar dataKey="playwright" fill="var(--color-playwright)" radius={2}>
						<ErrorBar dataKey="playwrightError" direction="y" width={4} />
					</Bar>}
					{drivers.includes("webmcp-scripted") && <Bar dataKey="scripted" fill="var(--color-scripted)" radius={2}>
						<ErrorBar dataKey="scriptedError" direction="y" width={4} />
					</Bar>}
					{drivers.includes("webmcp-jev") && <Bar dataKey="jev" fill="var(--color-jev)" radius={2}>
						<ErrorBar dataKey="jevError" direction="y" width={4} />
					</Bar>}
				</BarChart>
			</ChartContainer>
			<TableTwin title={t("benchmarks.tab.totals")} headers={headers} rows={scenarios.map(({ scenario, values }) => ({
				key: scenario,
				cells: [scenario, ...drivers.map((driver) => {
					const row = values[driver];
					return row ? `${formatMs(row.medianMs)} (${formatMs(row.minMs)}–${formatMs(row.maxMs)})` : "—";
				})],
			}))} />
		</div>
	);
}

function JevChart({ summary }: { summary: BenchSummaryRow[] }) {
	const { t } = useTranslation();
	const jevRows = scenariosFrom(summary).filter((scenario) => scenario.values["webmcp-jev"]);
	const data = jevRows.map(({ scenario, values }) => ({
		scenario,
		decision: values["webmcp-jev"]?.jev?.medianDecisionMs ?? undefined,
		execution: values["webmcp-jev"]?.jev?.medianExecMs ?? undefined,
		assertion: values["webmcp-jev"]?.jev?.medianAssertMs ?? undefined,
	}));
	const config: ChartConfig = {
		decision: { label: t("benchmarks.col.decisionMedian"), color: "var(--chart-1)" },
		execution: { label: t("benchmarks.col.executionMedian"), color: "var(--chart-3)" },
		assertion: { label: t("benchmarks.col.assertionMedian"), color: "var(--chart-5)" },
	};
	return (
		<div>
			<p className="mb-2 text-caption text-muted-foreground">{t("benchmarks.componentMedianNote")}</p>
			<ChartContainer config={config} className="h-[250px] w-full aspect-auto overflow-hidden">
				<BarChart data={data} margin={{ top: 8, right: 8, bottom: 4, left: 0 }}>
					<CartesianGrid vertical={false} stroke="var(--border)" />
					<XAxis dataKey="scenario" axisLine={false} tickLine={false} tick={{ fontSize: 10 }} interval={0} />
					<YAxis axisLine={false} tickLine={false} width={42} tick={{ fontSize: 10 }} />
					<RechartsTooltip content={<ChartTooltipContent />} />
					<ChartLegend content={<ChartLegendContent />} />
					<Bar dataKey="decision" fill="var(--color-decision)" radius={2} />
					<Bar dataKey="execution" fill="var(--color-execution)" radius={2} />
					<Bar dataKey="assertion" fill="var(--color-assertion)" radius={2} />
				</BarChart>
			</ChartContainer>
			<TableTwin title={t("benchmarks.tab.jev")} headers={[t("benchmarks.col.scenario"), t("benchmarks.col.decisionMedian"), t("benchmarks.col.executionMedian"), t("benchmarks.col.assertionMedian")]} rows={jevRows.map(({ scenario, values }) => {
				const jev = values["webmcp-jev"]?.jev;
				return { key: scenario, cells: [scenario, formatMs(jev?.medianDecisionMs), formatMs(jev?.medianExecMs), formatMs(jev?.medianAssertMs)] };
			})} />
		</div>
	);
}

function ScatterPoint(props: unknown) {
	const point = props as { cx?: number; cy?: number; payload?: BenchRecord };
	if (point.cx === undefined || point.cy === undefined || !point.payload) return null;
	const fill = point.payload.pass ? colors[point.payload.driver] : "var(--destructive)";
	const title = point.payload.pass ? "Pass" : "Fail";
	if (point.payload.driver === "playwright") {
		return <circle cx={point.cx} cy={point.cy} r={4} fill={fill} stroke="var(--background)" strokeWidth={1}><title>{title}</title></circle>;
	}
	if (point.payload.driver === "webmcp-scripted") {
		return <rect x={point.cx - 4} y={point.cy - 4} width={8} height={8} fill={fill} stroke="var(--background)" strokeWidth={1}><title>{title}</title></rect>;
	}
	return <path d={`M ${point.cx} ${point.cy - 5} L ${point.cx + 5} ${point.cy + 4} L ${point.cx - 5} ${point.cy + 4} Z`} fill={fill} stroke="var(--background)" strokeWidth={1}><title>{title}</title></path>;
}

function RepeatsChart({ records, drivers }: { records: BenchRecord[]; drivers: Driver[] }) {
	const { t } = useTranslation();
	const scenarios = [...new Set(records.map((record) => record.scenario))];
	const data = records.map((record) => ({ ...record, scenarioIndex: scenarios.indexOf(record.scenario) }));
	const config = totalsConfig(t);
	return (
		<div>
			<ChartContainer config={config} className="h-[250px] w-full aspect-auto overflow-hidden">
				<ScatterChart margin={{ top: 12, right: 8, bottom: 4, left: 0 }}>
					<CartesianGrid stroke="var(--border)" />
					<XAxis dataKey="scenarioIndex" type="number" domain={[-0.5, Math.max(0.5, scenarios.length - 0.5)]} ticks={scenarios.map((_, index) => index)} tickFormatter={(index) => scenarios[index] ?? ""} axisLine={false} tickLine={false} tick={{ fontSize: 9 }} />
					<YAxis dataKey="totalMs" type="number" axisLine={false} tickLine={false} width={42} tick={{ fontSize: 10 }} />
					<RechartsTooltip content={<ChartTooltipContent />} />
					<ChartLegend content={<ChartLegendContent />} />
					{drivers.map((driver) => <Scatter key={driver} name={driverLabel(driver, t)} dataKey="totalMs" data={data.filter((record) => record.driver === driver)} fill={colors[driver]} shape={<ScatterPoint />} />)}
				</ScatterChart>
			</ChartContainer>
			<TableTwin title={t("benchmarks.tab.repeats")} headers={[t("benchmarks.col.scenario"), t("benchmarks.col.driver"), t("benchmarks.col.repeat"), t("benchmarks.col.total"), t("benchmarks.col.result")]} rows={data.map((record, index) => ({
				key: `${record.scenario}-${record.driver}-${record.repeat}-${index}`,
				cells: [record.scenario, driverLabel(record.driver, t), record.repeat, formatMs(record.totalMs), t(record.pass ? "benchmarks.result.pass" : "benchmarks.result.fail")],
			}))} />
		</div>
	);
}

function PassRateChart({ summary, drivers }: { summary: BenchSummaryRow[]; drivers: Driver[] }) {
	const { t } = useTranslation();
	const data = aggregateDriverPassRates(summary).filter((entry) => drivers.includes(entry.driver)).map((entry) => ({
		...entry,
		key: driverKey(entry.driver),
		label: driverLabel(entry.driver, t),
		percent: entry.passRate === null ? 0 : entry.passRate * 100,
		result: entry.attempts === 0 ? "—" : `${entry.passes}/${entry.attempts}`,
	}));
	const config = totalsConfig(t);
	return (
		<div>
			<ChartContainer config={config} className="h-[230px] w-full aspect-auto overflow-hidden">
				<BarChart data={data} layout="vertical" margin={{ top: 8, right: 52, bottom: 4, left: 8 }}>
					<CartesianGrid horizontal={false} stroke="var(--border)" />
					<XAxis type="number" domain={[0, 100]} unit="%" axisLine={false} tickLine={false} tick={{ fontSize: 10 }} />
					<YAxis type="category" dataKey="label" width={108} axisLine={false} tickLine={false} tick={{ fontSize: 10 }} />
					<RechartsTooltip content={<ChartTooltipContent />} />
					<Bar dataKey="percent" radius={2}>
						{data.map((entry) => <Cell key={entry.driver} fill={`var(--color-${entry.key})`} />)}
						<LabelList dataKey="result" position="right" className="fill-muted-foreground" fontSize={10} />
					</Bar>
				</BarChart>
			</ChartContainer>
			<TableTwin title={t("benchmarks.tab.passRate")} headers={[t("benchmarks.col.driver"), t("benchmarks.col.passRate"), t("benchmarks.col.attempts")]} rows={data.map((entry) => ({
				key: entry.driver,
				cells: [entry.label, entry.passRate === null ? "—" : `${Math.round(entry.passRate * 100)}%`, `${entry.result} ${t("benchmarks.col.passed")}`],
			}))} />
		</div>
	);
}

export function BenchmarkChartPanel({ tab, summary, records, drivers }: {
	tab: "totals" | "jev" | "repeats" | "passRate";
	summary: BenchSummaryRow[];
	records: BenchRecord[];
	drivers: Driver[];
}) {
	if (tab === "totals") return <TotalsChart summary={summary} records={records} drivers={drivers} />;
	if (tab === "jev") return <JevChart summary={summary} />;
	if (tab === "repeats") return <RepeatsChart records={records} drivers={drivers} />;
	return <PassRateChart summary={summary} drivers={drivers} />;
}
