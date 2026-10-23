import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import type { MessageKey } from "../../i18n";
import { filterBenchmarkData, type BenchmarkPageFilter } from "../../benchmarks/aggregate";
import { useBenchmarkData } from "../../benchmarks/load";
import { DRIVERS, type Driver } from "../../benchmarks/record";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "../ui/select";
import { Switch } from "../ui/switch";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "../ui/tabs";
import { BenchmarkChartPanel } from "./BenchmarksCharts";
import { SummaryTable } from "./SummaryTable";

const pages: BenchmarkPageFilter[] = ["all", "board", "settings", "chat", "overhead"];
const tabs = ["totals", "jev", "repeats", "passRate"] as const;

const driverLabelKeys: Record<Driver, MessageKey> = {
	playwright: "benchmarks.driver.playwright",
	"webmcp-scripted": "benchmarks.driver.webmcpScripted",
	"webmcp-jev": "benchmarks.driver.webmcpJev",
};

function driverId(driver: Driver): string {
	return `benchmark-driver-${driver}`;
}

function formatTimestamp(value: string): string {
	const date = new Date(value);
	return Number.isNaN(date.getTime()) ? value : date.toLocaleString();
}

export function BenchmarksView() {
	const { t } = useTranslation();
	const data = useBenchmarkData();
	const [page, setPage] = useState<BenchmarkPageFilter>("all");
	const [drivers, setDrivers] = useState<Set<Driver>>(() => new Set(DRIVERS));
	const visibleDrivers = DRIVERS.filter((driver) => drivers.has(driver));
	const filtered = useMemo(
		() => filterBenchmarkData(data.records, data.summary, page, drivers),
		[data.records, data.summary, drivers, page],
	);
	const fixture = data.records[0];
	const toggleDriver = (driver: Driver, checked: boolean) => {
		setDrivers((current) => {
			const next = new Set(current);
			if (checked) next.add(driver);
			else next.delete(driver);
			return next;
		});
	};

	return (
		<main className="mx-auto flex min-h-0 w-full max-w-6xl flex-1 flex-col gap-5 overflow-y-auto px-4 py-5 md:px-6" data-testid="benchmarks-page">
			<header className="flex flex-col gap-2">
				<div>
					<h1 className="font-heading text-xl font-semibold">{t("benchmarks.title")}</h1>
					<p className="mt-1 text-sm text-muted-foreground">{t("benchmarks.subtitle")}</p>
				</div>
				{fixture && (
					<dl className="flex flex-wrap gap-x-4 gap-y-1 text-caption text-muted-foreground" data-testid="benchmark-run-metadata">
						<div className="flex gap-1"><dt>{t("benchmarks.meta.label")}</dt><dd>{fixture.label}</dd></div>
						<div className="flex gap-1"><dt>{t("benchmarks.meta.commit")}</dt><dd className="font-mono">{fixture.commit}</dd></div>
						<div className="flex gap-1"><dt>{t("benchmarks.meta.timestamp")}</dt><dd>{formatTimestamp(fixture.timestamp)}</dd></div>
						<div className="flex gap-1"><dt>{t("benchmarks.meta.environment")}</dt><dd>{fixture.env.platform}/{fixture.env.arch} · Chromium {fixture.env.chromium} · {fixture.env.viewport}</dd></div>
					</dl>
					)}
			</header>

			{data.error || data.summary.length === 0 ? (
				<p className="border-t border-border py-5 text-sm text-muted-foreground" role="status">{t("benchmarks.empty")}</p>
			) : (
				<>
					<section aria-label={t("benchmarks.summary")} className="min-w-0 border-t border-border pt-4">
						<div className="mb-3 flex flex-wrap items-end justify-between gap-3">
							<div className="flex flex-wrap items-end gap-3">
								<label className="grid gap-1 text-caption text-muted-foreground" htmlFor="benchmark-page-filter">
									<span>{t("benchmarks.filter.page")}</span>
									<Select value={page} onValueChange={(value) => setPage(value as BenchmarkPageFilter)}>
										<SelectTrigger id="benchmark-page-filter" aria-label={t("benchmarks.filter.page")} size="sm" className="min-w-32">
											<SelectValue />
										</SelectTrigger>
										<SelectContent>
											{pages.map((value) => <SelectItem key={value} value={value}>{t(`benchmarks.page.${value}`)}</SelectItem>)}
										</SelectContent>
									</Select>
								</label>
							</div>
							<div className="flex flex-wrap gap-x-3 gap-y-2" aria-label={t("benchmarks.filter.drivers")}>
								{DRIVERS.map((driver) => (
									<label key={driver} htmlFor={driverId(driver)} className="flex items-center gap-2 text-caption">
										<Switch id={driverId(driver)} checked={drivers.has(driver)} onCheckedChange={(checked) => toggleDriver(driver, checked)} data-testid={`benchmark-driver-toggle-${driver}`} />
										<span>{t(driverLabelKeys[driver])}</span>
									</label>
								))}
							</div>
						</div>
						<SummaryTable rows={filtered.summary} drivers={visibleDrivers} />
					</section>

					<section className="min-w-0 border-t border-border pt-4" aria-label={t("benchmarks.charts")}>
						<Tabs defaultValue="totals">
							<TabsList className="w-full justify-start overflow-x-auto rounded-md">
								{tabs.map((tab) => <TabsTrigger key={tab} value={tab} className="min-w-0 px-2 text-xs sm:px-3">{t(`benchmarks.tab.${tab}`)}</TabsTrigger>)}
							</TabsList>
							{tabs.map((tab) => (
								<TabsContent key={tab} value={tab} className="mt-3 min-w-0">
									<div className="min-w-0">
										<BenchmarkChartPanel tab={tab} summary={filtered.summary} records={filtered.records} drivers={visibleDrivers} />
									</div>
								</TabsContent>
							))}
						</Tabs>
					</section>
				</>
			)}
		</main>
	);
}
