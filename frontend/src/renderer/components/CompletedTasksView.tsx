import { useState, type ReactNode } from "react";
import { ScrollArea } from "radix-ui";
import { useTranslation } from "react-i18next";
import type { TFunction } from "i18next";
import { ArrowLeft, ListChecks } from "lucide-react";
import { Button } from "./ui/button";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "./ui/table";
import { topbarProjectLabelClass } from "./TopbarButton";
import {
	useCompletedTaskReport,
	useCompletedTasks,
	type CompletedTask,
	type CompletedTaskOutcome,
	type CompletedTaskProjectRef,
	type CompletedTaskReport,
	type HostedCompletedTask,
} from "../hooks/useCompletedTasks";
import { labelForHost } from "../lib/host-clients";
import { isLocal, refKey } from "../lib/hosts";
import { hidesShellTopbar } from "../lib/platform";
import { cn } from "../lib/utils";

const OUTCOMES: CompletedTaskOutcome[] = ["merged", "closed", "cancelled", "failed"];
const STAGES = ["building", "validating", "needs_review", "ready"] as const;

export type CompletedTaskSelection = { ref: CompletedTaskProjectRef; sessionId: string };

type Completeness = "complete" | "partial" | "unavailable";

export function formatDuration(t: TFunction, seconds: number | null | undefined): string {
	if (seconds === null || seconds === undefined) return t("completedTasks.unavailable");
	const h = Math.floor(seconds / 3600);
	const m = Math.floor((seconds % 3600) / 60);
	const s = seconds % 60;
	if (h > 0) return t("completedTasks.duration.hours", { h, m: String(m).padStart(2, "0") });
	if (m > 0) return t("completedTasks.duration.minutes", { m, s: String(s).padStart(2, "0") });
	return t("completedTasks.duration.seconds", { s });
}

function formatCount(t: TFunction, value: number | null | undefined, locale?: string): string {
	if (value === null || value === undefined) return t("completedTasks.unavailable");
	return new Intl.NumberFormat(locale).format(value);
}

function formatTime(t: TFunction, value: string | null | undefined, locale?: string): string {
	if (!value) return t("completedTasks.unavailable");
	return new Intl.DateTimeFormat(locale, { dateStyle: "medium", timeStyle: "short" }).format(new Date(value));
}

function hostLabel(t: TFunction, ref: CompletedTaskProjectRef): string {
	return isLocal(ref.hostId) ? t("completedTasks.thisMachine") : labelForHost(ref.hostId) ?? ref.hostId;
}

function CompletenessNote({ value }: { value: Completeness }) {
	const { t } = useTranslation();
	if (value === "complete") return null;
	return (
		<span className="text-xs font-normal text-muted-foreground" title={t(`completedTasks.completenessHint.${value}`)}>
			{t(value === "partial" ? "completedTasks.partial" : "completedTasks.unavailable")}
		</span>
	);
}

// Completed work across a project's associated hosts. Each host's daemon
// derives its reports; this view only lists and renders them.
export function CompletedTasksView({
	refs,
	selected,
	onSelect,
}: {
	refs: CompletedTaskProjectRef[];
	selected: CompletedTaskSelection | null;
	onSelect: (selection: CompletedTaskSelection | null) => void;
}) {
	const { t } = useTranslation();
	const showPageTitle = hidesShellTopbar();
	return (
		<div className="flex min-h-0 flex-1 flex-col bg-background">
			{!showPageTitle && !selected ? <h1 className="sr-only">{t("completedTasks.title")}</h1> : null}
			{showPageTitle || selected ? <header className="workspace-topbar-container center-panel-titlebar flex h-toolbar shrink-0 items-center gap-2 border-b border-border-strong pr-2">
				{selected ? (
					<Button variant="ghost" size="icon-sm" aria-label={t("completedTasks.back")} title={t("completedTasks.back")} onClick={() => onSelect(null)}>
						<ArrowLeft className="size-icon-md" aria-hidden="true" />
					</Button>
				) : null}
				<h1 className={showPageTitle ? cn(topbarProjectLabelClass, "inline-flex items-center gap-1.5") : "sr-only"}>
					{showPageTitle ? <ListChecks className="size-icon-md" aria-hidden="true" /> : null}
					{t("completedTasks.title")}
				</h1>
			</header> : null}
			<ScrollArea.Root asChild type="hover" scrollHideDelay={500}>
				<main className="relative min-h-0 flex-1 overflow-hidden">
					<ScrollArea.Viewport className="h-full w-full overscroll-contain focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-ring/60" tabIndex={0}>
						<div className="p-4">
							{selected ? <CompletedTaskReportView selection={selected} /> : <CompletedTaskList refs={refs} onSelect={onSelect} />}
						</div>
					</ScrollArea.Viewport>
					<ScrollArea.Scrollbar orientation="vertical" className="z-10 flex w-2 touch-none select-none p-0.5">
						<ScrollArea.Thumb className="relative flex-1 rounded-full bg-[color-mix(in_srgb,var(--color-scrollbar)_42%,transparent)] hover:bg-[color-mix(in_srgb,var(--color-scrollbar)_56%,transparent)]" />
					</ScrollArea.Scrollbar>
				</main>
			</ScrollArea.Root>
		</div>
	);
}

function CompletedTaskList({ refs, onSelect }: { refs: CompletedTaskProjectRef[]; onSelect: (selection: CompletedTaskSelection) => void }) {
	const { t, i18n } = useTranslation();
	const [outcome, setOutcome] = useState<CompletedTaskOutcome | undefined>(undefined);
	const [pages, setPages] = useState(1);
	const { tasks, sources, hasMore, isFetching } = useCompletedTasks(refs, outcome, pages);
	const failed = sources.filter((source) => source.status === "error");
	const loading = sources.some((source) => source.status === "loading");
	const allFailed = failed.length === sources.length && sources.length > 0;
	const locale = i18n.resolvedLanguage;
	const multiHost = new Set(refs.map((ref) => ref.hostId)).size > 1;

	return (
		<>
			<div className="mb-3 flex items-center gap-1" role="group" aria-label={t("completedTasks.filter.label")}>
				{([undefined, ...OUTCOMES] as const).map((value) => (
					<button
						key={value ?? "all"}
						type="button"
						aria-pressed={outcome === value}
						onClick={() => {
							setOutcome(value);
							setPages(1);
						}}
						className={cn(
							"inline-flex h-7 items-center rounded-md px-2.5 text-xs transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/60",
							outcome === value ? "bg-muted text-foreground" : "text-muted-foreground hover:bg-muted/50 hover:text-foreground",
						)}
					>
						{value ? t(`completedTasks.outcome.${value}`) : t("completedTasks.filter.all")}
					</button>
				))}
			</div>
			{failed.length > 0 ? (
				<div role={allFailed ? "alert" : "status"} className="mb-3 rounded-md bg-destructive/10 px-3 py-2 text-xs text-destructive">
					<p>{allFailed ? t("completedTasks.loadError") : t("completedTasks.partialHosts")}</p>
					{failed.map((source) => (
						<p key={refKey({ host: source.ref.hostId, id: source.ref.projectId })}>
							{t("completedTasks.hostError", { host: hostLabel(t, source.ref), error: source.error ?? "" })}
						</p>
					))}
				</div>
			) : null}
			{loading && tasks.length === 0 ? (
				<p role="status" className="px-3 py-8 text-center text-sm text-muted-foreground">
					{t("completedTasks.loading")}
				</p>
			) : null}
			{!loading && !allFailed && tasks.length === 0 ? (
				<p role="status" className="px-3 py-8 text-center text-sm text-muted-foreground">
					{outcome ? t("completedTasks.emptyFiltered") : t("completedTasks.empty")}
				</p>
			) : null}
			{tasks.length > 0 ? (
				<Table className="text-xs">
					<TableHeader>
						<TableRow className="hover:bg-transparent">
							<TableHead className="text-muted-foreground">{t("completedTasks.column.task")}</TableHead>
							<TableHead className="text-muted-foreground">{t("completedTasks.column.outcome")}</TableHead>
							<TableHead className="text-muted-foreground">{t("completedTasks.column.completed")}</TableHead>
							<TableHead className="text-right text-muted-foreground">{t("completedTasks.column.elapsed")}</TableHead>
							<TableHead className="text-right text-muted-foreground">{t("completedTasks.column.input")}</TableHead>
							<TableHead className="text-right text-muted-foreground">{t("completedTasks.column.output")}</TableHead>
							<TableHead className="text-muted-foreground">{t("completedTasks.column.agent")}</TableHead>
						</TableRow>
					</TableHeader>
					<TableBody>
						{tasks.map((task) => (
							<CompletedTaskRow key={refKey({ host: task.ref.hostId, id: task.sessionId })} task={task} showHost={multiHost} locale={locale} onSelect={onSelect} />
						))}
					</TableBody>
				</Table>
			) : null}
			{hasMore ? (
				<div className="mt-3 flex justify-center">
					<Button variant="ghost" size="sm" disabled={isFetching} onClick={() => setPages((current) => current + 1)}>
						{t("completedTasks.loadMore")}
					</Button>
				</div>
			) : null}
		</>
	);
}

function CompletedTaskRow({
	task,
	showHost,
	locale,
	onSelect,
}: {
	task: HostedCompletedTask;
	showHost: boolean;
	locale?: string;
	onSelect: (selection: CompletedTaskSelection) => void;
}) {
	const { t } = useTranslation();
	const partialTokens = task.tokens.completeness === "partial";
	return (
		<TableRow>
			<TableCell className="max-w-0 w-[32%]">
				<button
					type="button"
					className="block w-full truncate rounded-sm text-left text-[13px] font-medium text-foreground hover:underline focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/60"
					aria-label={t("completedTasks.openReport", { title: task.title })}
					onClick={() => onSelect({ ref: task.ref, sessionId: task.sessionId })}
				>
					{task.title}
				</button>
				<p className="truncate text-[11px] text-muted-foreground">
					<span className="font-mono">{task.sessionId}</span>
					{showHost ? ` · ${hostLabel(t, task.ref)}` : ""}
					{task.prs.map((pr) => ` · #${pr.number}`).join("")}
				</p>
			</TableCell>
			<TableCell>{t(`completedTasks.outcome.${task.outcome}`)}</TableCell>
			<TableCell className={cn("tabular-nums", !task.completedAt && "text-muted-foreground")}>{formatTime(t, task.completedAt, locale)}</TableCell>
			<TableCell className={cn("text-right tabular-nums", task.elapsedSeconds == null && "text-muted-foreground")}>{formatDuration(t, task.elapsedSeconds)}</TableCell>
			<TableCell className={cn("text-right tabular-nums", task.tokens.inputTokens === null && "text-muted-foreground")} title={partialTokens ? t("completedTasks.completenessHint.partial") : undefined}>
				{formatCount(t, task.tokens.inputTokens, locale)}
				{partialTokens && task.tokens.inputTokens !== null ? "+" : ""}
			</TableCell>
			<TableCell className={cn("text-right tabular-nums", task.tokens.outputTokens === null && "text-muted-foreground")} title={partialTokens ? t("completedTasks.completenessHint.partial") : undefined}>
				{formatCount(t, task.tokens.outputTokens, locale)}
				{partialTokens && task.tokens.outputTokens !== null ? "+" : ""}
			</TableCell>
			<TableCell className="max-w-0 w-[22%] truncate text-muted-foreground" title={agentLabel(t, task)}>{agentLabel(t, task)}</TableCell>
		</TableRow>
	);
}

function agentLabel(t: TFunction, config: Pick<CompletedTask, "harness" | "model" | "effort">): string {
	return [config.harness || t("completedTasks.defaultValue"), config.model || t("completedTasks.defaultValue"), config.effort || t("completedTasks.defaultValue")].join(" · ");
}

function Section({ title, completeness, children }: { title: string; completeness?: Completeness; children: ReactNode }) {
	return (
		<section className="border-b border-border py-4 first:pt-0 last:border-b-0">
			<h3 className="mb-2 flex items-baseline gap-2 text-sm font-medium text-foreground">
				{title}
				{completeness ? <CompletenessNote value={completeness} /> : null}
			</h3>
			{children}
		</section>
	);
}

function Fact({ label, children }: { label: string; children: ReactNode }) {
	return (
		<div className="grid grid-cols-[200px_minmax(0,1fr)] gap-3 py-1 text-xs">
			<dt className="text-muted-foreground">{label}</dt>
			<dd className="min-w-0 text-foreground">{children}</dd>
		</div>
	);
}

function CompletedTaskReportView({ selection }: { selection: CompletedTaskSelection }) {
	const { t } = useTranslation();
	const query = useCompletedTaskReport(selection.ref, selection.sessionId);
	if (query.isLoading) {
		return (
			<p role="status" className="px-3 py-8 text-center text-sm text-muted-foreground">
				{t("completedTasks.loadingReport")}
			</p>
		);
	}
	if (query.error || !query.data) {
		return (
			<p role="alert" className="text-sm text-destructive">
				{query.error?.message ?? t("completedTasks.loadError")}
			</p>
		);
	}
	return <ReportBody report={query.data} selection={selection} />;
}

export function ReportBody({ report, selection }: { report: CompletedTaskReport; selection: CompletedTaskSelection }) {
	const { t, i18n } = useTranslation();
	const locale = i18n.resolvedLanguage;
	const totals = new Map(report.stages.totals.map((total) => [total.stage, total]));
	return (
		<article aria-labelledby="completed-task-title" className="max-w-4xl">
			<h2 id="completed-task-title" className="mb-4 text-base font-semibold text-foreground">
				{report.title}
			</h2>
			<Section title={t("completedTasks.report.summary")}>
				<dl>
					<Fact label={t("completedTasks.column.outcome")}>{t(`completedTasks.outcome.${report.outcome}`)}</Fact>
					<Fact label={t("completedTasks.report.machine")}>
						{hostLabel(t, selection.ref)} <span className="font-mono text-muted-foreground">{report.hostId}</span>
					</Fact>
					<Fact label={t("completedTasks.report.worker")}>
						<span className="font-mono">{report.sessionId}</span>
					</Fact>
					{report.issueId ? <Fact label={t("completedTasks.report.issue")}>{report.issueId}</Fact> : null}
					{report.prs.length > 0 ? (
						<Fact label={t("completedTasks.report.prs")}>
							{report.prs.map((pr) => (
								<a key={pr.url} href={pr.url} target="_blank" rel="noreferrer" className="mr-3 text-[var(--color-text-markdown-link)] hover:underline">
									{t("completedTasks.report.prLink", { number: pr.number, state: t(`completedTasks.prState.${pr.state}`) })}
								</a>
							))}
						</Fact>
					) : null}
					<Fact label={t("completedTasks.report.started")}>
						<span className="tabular-nums">{formatTime(t, report.createdAt, locale)}</span>
					</Fact>
					<Fact label={t("completedTasks.column.completed")}>
						<span className="tabular-nums">{formatTime(t, report.completedAt, locale)}</span>
					</Fact>
					<Fact label={t("completedTasks.column.elapsed")}>
						<span className="tabular-nums">{formatDuration(t, report.elapsedSeconds)}</span>
					</Fact>
				</dl>
			</Section>

			<Section title={t("completedTasks.report.configuration")} completeness={report.config.completeness}>
				{report.config.segments.length === 0 ? (
					<p className="text-xs text-muted-foreground">
						{t("completedTasks.report.finalConfig", { value: agentLabel(t, report) })}
					</p>
				) : (
					<Table className="text-xs">
						<TableHeader>
							<TableRow className="hover:bg-transparent">
								<TableHead className="text-muted-foreground">{t("completedTasks.report.since")}</TableHead>
								<TableHead className="text-muted-foreground">{t("completedTasks.report.harness")}</TableHead>
								<TableHead className="text-muted-foreground">{t("completedTasks.report.model")}</TableHead>
								<TableHead className="text-muted-foreground">{t("completedTasks.report.effort")}</TableHead>
								<TableHead className="text-muted-foreground">{t("completedTasks.report.source")}</TableHead>
							</TableRow>
						</TableHeader>
						<TableBody>
							{report.config.segments.map((segment, index) => (
								<TableRow key={`${segment.since}-${index}`}>
									<TableCell className="tabular-nums">{formatTime(t, segment.since, locale)}</TableCell>
									<TableCell>{segment.harness || t("completedTasks.defaultValue")}</TableCell>
									<TableCell className="font-mono">{segment.model || t("completedTasks.defaultValue")}</TableCell>
									<TableCell>{segment.effort || t("completedTasks.defaultValue")}</TableCell>
									<TableCell className="text-muted-foreground">{t(`completedTasks.report.sourceKind.${segment.source}`)}</TableCell>
								</TableRow>
							))}
						</TableBody>
					</Table>
				)}
			</Section>

			<Section title={t("completedTasks.report.usage")} completeness={report.tokens.completeness}>
				<dl>
					<Fact label={t("completedTasks.report.input")}>
						<span className="tabular-nums">{formatCount(t, report.tokens.inputTokens, locale)}</span>
					</Fact>
					<Fact label={t("completedTasks.report.cachedInput")}>
						<span className="tabular-nums">{formatCount(t, report.tokens.cachedInputTokens, locale)}</span>
					</Fact>
					<Fact label={t("completedTasks.report.output")}>
						<span className="tabular-nums">{formatCount(t, report.tokens.outputTokens, locale)}</span>
					</Fact>
				</dl>
				{report.runs.length > 0 ? (
					<Table className="mt-2 text-xs">
						<TableHeader>
							<TableRow className="hover:bg-transparent">
								<TableHead className="text-muted-foreground">{t("completedTasks.report.run")}</TableHead>
								<TableHead className="text-muted-foreground">{t("completedTasks.report.model")}</TableHead>
								<TableHead className="text-right text-muted-foreground">{t("completedTasks.report.input")}</TableHead>
								<TableHead className="text-right text-muted-foreground">{t("completedTasks.report.cached")}</TableHead>
								<TableHead className="text-right text-muted-foreground">{t("completedTasks.report.output")}</TableHead>
								<TableHead className="text-muted-foreground" />
							</TableRow>
						</TableHeader>
						<TableBody>
							{report.runs.map((run) => (
								<TableRow key={`${run.bindingId}-${run.model}`}>
									<TableCell className="max-w-0 w-[40%] truncate font-mono" title={run.nativeRootId}>
										{run.harness} · {run.nativeRootId}
									</TableCell>
									<TableCell className="font-mono">{run.model}</TableCell>
									<TableCell className="text-right tabular-nums">{formatCount(t, run.inputTokens, locale)}</TableCell>
									<TableCell className="text-right tabular-nums">{formatCount(t, run.cachedInputTokens, locale)}</TableCell>
									<TableCell className="text-right tabular-nums">{formatCount(t, run.outputTokens, locale)}</TableCell>
									<TableCell>
										<CompletenessNote value={run.completeness} />
									</TableCell>
								</TableRow>
							))}
						</TableBody>
					</Table>
				) : null}
			</Section>

			<Section title={t("completedTasks.report.stages")} completeness={report.stages.completeness}>
				{report.stages.completeness === "unavailable" ? (
					<p className="text-xs text-muted-foreground">{t("completedTasks.completenessHint.unavailable")}</p>
				) : (
					<>
						<dl>
							{STAGES.map((stage) => {
								const total = totals.get(stage);
								return (
									<Fact key={stage} label={t(`completedTasks.stage.${stage}`)}>
										<span className="tabular-nums">
											{total
												? t("completedTasks.report.stageTotal", { duration: formatDuration(t, total.seconds), count: total.visits })
												: t("completedTasks.report.notVisited")}
										</span>
										{total?.hasUnfinished ? <span className="ml-2 text-muted-foreground">{t("completedTasks.report.ongoing")}</span> : null}
									</Fact>
								);
							})}
						</dl>
						<p className="mt-1 text-xs text-muted-foreground">{t("completedTasks.report.stageNote")}</p>
						<h4 className="mt-3 mb-1 text-xs font-medium text-foreground">{t("completedTasks.report.timeline")}</h4>
						<ol className="text-xs">
							{report.stages.visits.map((visit, index) => (
								<li key={`${visit.enteredAt}-${index}`} className="grid grid-cols-[200px_minmax(0,1fr)_auto] gap-3 border-b border-border py-1 last:border-b-0">
									<span>{t(`completedTasks.stage.${visit.stage}`)}</span>
									<span className="tabular-nums text-muted-foreground">
										{formatTime(t, visit.enteredAt, locale)} – {visit.leftAt ? formatTime(t, visit.leftAt, locale) : t("completedTasks.report.ongoing")}
									</span>
									<span className="text-right tabular-nums">{formatDuration(t, visit.durationSeconds)}</span>
								</li>
							))}
						</ol>
					</>
				)}
			</Section>
		</article>
	);
}
