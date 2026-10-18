import { useCallback, useEffect, useLayoutEffect, useRef } from "react";
import { useTranslation } from "react-i18next";
import { useWorkerReports } from "../../hooks/useWorkerReports";
import { artifactLink, artifactLinkForPath, parseArtifactLink, sessionLink } from "../../lib/session-links";
import { useSessionLinkNavigation, useSessionLinkSource } from "../../lib/use-session-link-navigation";
import { labelForHost } from "../../lib/host-clients";
import type { MessageKey } from "../../i18n/messages";
import { Badge } from "../ui/badge";

const statusKeys: Record<string, MessageKey> = {
	checkpoint: "reports.status.checkpoint",
	done: "reports.status.done",
	needs_input: "reports.status.needsInput",
	stuck: "reports.status.stuck",
};

export function WorkerReportsPanel({ projectId, ownerHostId }: { projectId: string; ownerHostId?: string }) {
	const { t } = useTranslation();
	const { data = [], isLoading, error, refetch, fetchNextPage, hasNextPage, isFetchingNextPage, isFetchNextPageError } = useWorkerReports(projectId, ownerHostId);
	const scrollContainerRef = useRef<HTMLDivElement | null>(null);
	const loadMoreRef = useRef<HTMLLIElement | null>(null);
	const loadMorePendingRef = useRef(false);
	const scrollAnchorRef = useRef<{ key: string; top: number } | null>(null);
	const secondLastIndex = data.length >= 2 ? data.length - 2 : -1;
	const listIdentity = data.map(reportKey).join("\u0000");
	const loadMore = useCallback(async () => {
		if (!hasNextPage || isFetchingNextPage || loadMorePendingRef.current) return;
		loadMorePendingRef.current = true;
		try {
			await fetchNextPage({ cancelRefetch: false });
		} finally {
			loadMorePendingRef.current = false;
		}
	}, [fetchNextPage, hasNextPage, isFetchingNextPage]);
	useEffect(() => {
		const node = loadMoreRef.current;
		if (!node || !hasNextPage || isFetchingNextPage || isFetchNextPageError) return;
		const observer = new IntersectionObserver(
			(entries) => {
				if (entries.some((entry) => entry.isIntersecting)) {
					void loadMore();
				}
			},
			{ root: scrollContainerRef.current, rootMargin: "120px" },
		);
		observer.observe(node);
		return () => observer.disconnect();
	}, [hasNextPage, isFetchingNextPage, isFetchNextPageError, loadMore, secondLastIndex]);
	useLayoutEffect(() => {
		const anchor = scrollAnchorRef.current;
		const container = scrollContainerRef.current;
		if (!anchor || !container) return;
		const element = Array.from(container.querySelectorAll<HTMLElement>("[data-report-key]"))
			.find((candidate) => candidate.dataset.reportKey === anchor.key);
		if (element) container.scrollTop += element.getBoundingClientRect().top - anchor.top;
	}, [listIdentity]);
	return (
		<aside aria-label={t("reports.title")} className="flex h-full min-h-0 min-w-0 flex-col border-l border-border bg-background" data-testid="worker-reports-panel">
			<h2 className="shrink-0 border-b border-border px-4 py-3 text-sm font-semibold">{t("reports.title")}</h2>
			<div className="min-h-0 flex-1 overflow-y-auto overscroll-contain" ref={scrollContainerRef} onScroll={() => {
				const container = scrollContainerRef.current;
				if (!container) return;
				const top = container.getBoundingClientRect().top;
				const anchor = Array.from(container.querySelectorAll<HTMLElement>("[data-report-key]"))
					.find((candidate) => candidate.getBoundingClientRect().bottom > top);
				if (anchor?.dataset.reportKey) scrollAnchorRef.current = { key: anchor.dataset.reportKey, top: anchor.getBoundingClientRect().top };
			}}>
				{isLoading ? <p className="p-4 text-sm text-muted-foreground" role="status">{t("reports.loading")}</p> : null}
				{error && data.length === 0 ? <div className="p-4 text-sm text-destructive" role="alert"><p>{t("reports.loadError")}</p><button className="mt-2 underline underline-offset-2 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring" onClick={() => void refetch()} type="button">{t("reports.retry")}</button></div> : null}
				{!isLoading && !error && data.length === 0 ? <p className="p-4 text-sm text-muted-foreground">{t("reports.empty")}</p> : null}
				<ol className="divide-y divide-border">
					{data.map((report, index) => {
						const sourceHostId = report.hostId;
						const machine = report.hostLabel || (sourceHostId && sourceHostId !== "local" ? labelForHost(sourceHostId) || sourceHostId : t("reports.thisMachine"));
						const machineIdentity = sourceHostId && machine !== sourceHostId ? `${machine} (${sourceHostId})` : machine;
						const source = [machineIdentity, report.agent || t("reports.unknownAgent"), report.sessionName || report.sessionId].join(" · ");
						const body = report.note || report.message;
						const sessionHref = sessionLink(report.sourceProjectId || report.projectId, report.sessionId, sourceHostId);
						return <li className="grid min-w-0 grid-cols-[minmax(7rem,0.8fr)_minmax(0,2fr)] gap-3 px-3 py-3 text-sm max-[700px]:grid-cols-1" data-report-key={reportKey(report)} key={reportKey(report)} ref={index === secondLastIndex ? loadMoreRef : undefined}>
							<div className="min-w-0 text-xs text-muted-foreground"><p className="break-words font-medium text-foreground">{source}</p><ReportSessionLink href={sessionHref} hostId={sourceHostId} label={report.sessionId} /></div>
							<div className="min-w-0"><div className="flex flex-wrap items-center justify-between gap-2"><Badge variant="outline">{report.state ? t(statusKeys[report.state] ?? "reports.status.unknown") : t("reports.status.report")}</Badge><time className="text-xs text-muted-foreground" dateTime={report.createdAt}>{new Date(report.createdAt).toLocaleString()}</time></div>
								{body ? <p className="mt-2 whitespace-pre-wrap break-words">{body}</p> : null}
								{report.repeatCount > 1 ? <p className="mt-1 text-xs text-muted-foreground">{t("reports.repeated", { count: report.repeatCount })}</p> : null}
								{report.outputs?.length ? <ReportOutputs hostId={sourceHostId} projectId={report.sourceProjectId || report.projectId} sessionId={report.sessionId} outputs={report.outputs} /> : null}
							</div>
						</li>;
					})}
				</ol>
				{isFetchNextPageError ? <div className="p-3 text-sm text-destructive" role="alert"><p>{t("reports.loadMoreError")}</p><button className="mt-2 underline underline-offset-2 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring" onClick={() => void loadMore()} type="button">{t("reports.retry")}</button></div> : null}
				{isFetchingNextPage ? <p className="p-3 text-center text-sm text-muted-foreground" role="status">{t("reports.loadingMore")}</p> : null}
				{hasNextPage && !isFetchingNextPage && !isFetchNextPageError ? <button className="w-full p-3 text-sm text-primary underline underline-offset-2 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring" onClick={() => void loadMore()} type="button">{t("reports.loadMore")}</button> : null}
			</div>
		</aside>
	);
}

function ReportSessionLink({ href, hostId, label }: { href: string; hostId?: string; label: string }) {
	const navigate = useSessionLinkNavigation(hostId);
	return <a className="mt-1 inline-block break-all underline underline-offset-2 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring" href={href} onClick={(event) => { event.preventDefault(); navigate(href); }}>{label}</a>;
}

function ReportArtifactLink({ href, hostId, label }: { href: string; hostId?: string; label: string }) {
	const navigate = useSessionLinkNavigation(hostId);
	return <a className="break-all underline underline-offset-2 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring" href={href} onClick={(event) => { event.preventDefault(); navigate(href); }}>{label}</a>;
}

function ReportOutputs({ hostId, projectId, sessionId, outputs }: {
	hostId?: string;
	projectId: string;
	sessionId: string;
	outputs: Array<{ kind: string; reference: string; label?: string }>;
}) {
	const { workspaces } = useSessionLinkSource(hostId);
	const navigation = useSessionLinkNavigation(hostId);
	const artifactHost = hostId && hostId !== "local" ? hostId : undefined;
	return <ul className="mt-2 flex flex-wrap gap-2">{outputs.map((output, index) => {
		const label = output.label || output.reference;
		if (output.kind !== "artifact") return <li key={`${output.kind}-${output.reference}-${index}`}><a className="break-all text-primary underline underline-offset-2 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring" href={output.reference} target="_blank" rel="noreferrer">{label}</a></li>;
		const href = reportArtifactHref(output.reference, projectId, sessionId, artifactHost, workspaces);
		return <li key={`${output.kind}-${output.reference}-${index}`}>{href
			? <a className="break-all underline underline-offset-2 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring" href={href} onClick={(event) => { event.preventDefault(); navigation(href); }}>{label}</a>
			: <span className="break-all text-muted-foreground">{label}</span>}</li>;
	})}</ul>;
}

function reportArtifactHref(reference: string, projectId: string, sessionId: string, hostId: string | undefined, workspaces: Parameters<typeof artifactLinkForPath>[1]): string | undefined {
	const target = parseArtifactLink(reference);
	if (target) {
		if (target.projectId !== projectId || target.sessionId !== sessionId || (target.hostId && target.hostId !== hostId)) return undefined;
		return artifactLink(projectId, sessionId, target.path, hostId);
	}
	if (isSafeRelativeArtifactPath(reference)) return artifactLink(projectId, sessionId, reference, hostId);
	const historical = artifactLinkForPath(reference, workspaces);
	const normalized = historical ? parseArtifactLink(historical) : undefined;
	if (normalized?.projectId === projectId && normalized.sessionId === sessionId) {
		return artifactLink(projectId, sessionId, normalized.path, hostId);
	}
	return undefined;
}

function isSafeRelativeArtifactPath(reference: string): boolean {
	const path = reference.trim();
	return Boolean(path) && !path.startsWith("/") && !/^[A-Za-z]:[\\/]/.test(path) && !path.includes("\\") && path.split("/").every((segment) => segment.trim() !== "" && segment !== "." && segment !== "..");
}

function reportKey(report: { hostId?: string; sessionId: string; id: string }): string {
	return `${report.hostId || "local"}/${report.sessionId}/${report.id}`;
}
