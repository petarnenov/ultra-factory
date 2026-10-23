import { useTranslation } from "react-i18next";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "../ui/table";
import type { BenchSummaryRow, Driver } from "../../benchmarks/record";

type ScenarioRow = { scenario: string; page: string; values: Partial<Record<Driver, BenchSummaryRow>> };

function formatMs(value: number | null): string {
	return value === null ? "—" : `${Math.round(value)} ms`;
}

function formatRatio(value: number | null): string {
	return value === null ? "—" : `×${value.toFixed(2)}`;
}

export function SummaryTable({ rows, drivers }: { rows: BenchSummaryRow[]; drivers: Driver[] }) {
	const { t } = useTranslation();
	const scenarios = new Map<string, ScenarioRow>();
	for (const row of rows) {
		const item = scenarios.get(row.scenario) ?? { scenario: row.scenario, page: row.page, values: {} };
		item.values[row.driver] = row;
		scenarios.set(row.scenario, item);
	}
	return (
		<div className="min-w-0">
			<h2 className="mb-2 text-sm font-medium">{t("benchmarks.summary")}</h2>
			<Table aria-label={t("benchmarks.summary")} className="table-fixed text-caption">
				<TableHeader>
					<TableRow>
						<TableHead className="w-1/4 whitespace-normal">{t("benchmarks.col.scenario")}</TableHead>
						{drivers.map((driver) => (
							<TableHead key={driver} className="w-1/4 whitespace-normal" data-testid={`benchmark-driver-column-${driver}`}>
								{t(`benchmarks.driver.${driver === "playwright" ? "playwright" : driver === "webmcp-scripted" ? "webmcpScripted" : "webmcpJev"}`)}
							</TableHead>
						))}
					</TableRow>
				</TableHeader>
				<TableBody>
					{[...scenarios.values()].map((scenario) => (
						<TableRow key={scenario.scenario} data-testid="benchmark-summary-row" data-scenario={scenario.scenario}>
							<TableCell className="whitespace-normal break-words font-mono text-[11px]">{scenario.scenario}</TableCell>
							{drivers.map((driver) => {
								const row = scenario.values[driver];
								return (
									<TableCell key={driver} className="whitespace-normal p-1.5 text-[11px] leading-4 tabular-nums">
										{row ? (
											<div className="grid gap-0.5">
												<span className="font-medium">{formatMs(row.medianMs)}</span>
												<span className="text-muted-foreground">{formatMs(row.minMs)}–{formatMs(row.maxMs)}</span>
												<span className="text-muted-foreground">{t("benchmarks.col.pass")}: {Math.round(row.passRate * row.n)}/{row.n} · {formatRatio(row.ratioToPlaywright)}</span>
											</div>
									) : <span className="text-muted-foreground">—</span>}
									</TableCell>
								);
							})}
						</TableRow>
					))}
				</TableBody>
			</Table>
		</div>
	);
}
