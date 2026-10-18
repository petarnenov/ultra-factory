import { useEffect, useRef } from "react";
import { useTranslation } from "react-i18next";
import { useWorkerReports } from "../../hooks/useWorkerReports";
import { artifactLink } from "../../lib/session-links";
import { useSessionLinkNavigation } from "../../lib/use-session-link-navigation";
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
	const { data = [], isLoading, error, fetchNextPage, hasNextPage, isFetchingNextPage } = useWorkerReports(projectId, ownerHostId);
	const scrollContainerRef = useRef<HTMLDivElement | null>(null);
	const loadMoreRef = useRef<HTMLLIElement | null>(null);
	const secondLastIndex = data.length >= 2 ? data.length - 2 : -1;
	useEffect(() => {
		const node = loadMoreRef.current;
		if (!node || !hasNextPage || isFetchingNextPage) return;
		const observer = new IntersectionObserver(
			(entries) => {
				if (entries.some((entry) => entry.isIntersecting)) {
					void fetchNextPage();
				}
			},
			{ root: scrollContainerRef.current, rootMargin: "120px" },
		);
		observer.observe(node);
		return () => observer.disconnect();
	}, [fetchNextPage, hasNextPage, isFetchingNextPage, secondLastIndex]);
	return (
		<aside aria-label={t("reports.title")} className="flex h-full min-h-0 min-w-0 flex-col border-l border-border bg-background" data-testid="worker-reports-panel">
			<h2 className="shrink-0 border-b border-border px-4 py-3 text-sm font-semibold">{t("reports.title")}</h2>
			<div className="min-h-0 flex-1 overflow-y-auto overscroll-contain" ref={scrollContainerRef}>
				{isLoading ? <p className="p-4 text-sm text-muted-foreground" role="status">{t("reports.loading")}</p> : null}
				{error ? <p className="p-4 text-sm text-destructive" role="alert">{t("reports.loadError")}</p> : null}
				{!isLoading && !error && data.length === 0 ? <p className="p-4 text-sm text-muted-foreground">{t("reports.empty")}</p> : null}
				<ol className="divide-y divide-border">
					{data.map((report, index) => {
						const sourceHostId = report.hostId || ownerHostId;
						const machine = report.hostLabel || (sourceHostId ? labelForHost(sourceHostId) || sourceHostId : t("reports.thisMachine"));
						const machineIdentity = sourceHostId && machine !== sourceHostId ? `${machine} (${sourceHostId})` : machine;
						const source = [machineIdentity, report.agent || t("reports.unknownAgent"), report.sessionName || report.sessionId].join(" · ");
						const body = report.note || report.message;
						const sessionHref = `ao://sessions/${encodeURIComponent(report.sourceProjectId || report.projectId)}/${encodeURIComponent(report.sessionId)}`;
						return <li className="grid min-w-0 grid-cols-[minmax(7rem,0.8fr)_minmax(0,2fr)] gap-3 px-3 py-3 text-sm max-[700px]:grid-cols-1" key={`${report.hostId || "local"}/${report.sessionId}/${report.id}`} ref={index === secondLastIndex ? loadMoreRef : undefined}>
							<div className="min-w-0 text-xs text-muted-foreground"><p className="break-words font-medium text-foreground">{source}</p><ReportSessionLink href={sessionHref} hostId={sourceHostId} label={report.sessionId} /></div>
							<div className="min-w-0"><div className="flex flex-wrap items-center justify-between gap-2"><Badge variant="outline">{report.state ? t(statusKeys[report.state] ?? "reports.status.unknown") : t("reports.status.report")}</Badge><time className="text-xs text-muted-foreground" dateTime={report.createdAt}>{new Date(report.createdAt).toLocaleString()}</time></div>
								{body ? <p className="mt-2 whitespace-pre-wrap break-words">{body}</p> : null}
								{report.repeatCount > 1 ? <p className="mt-1 text-xs text-muted-foreground">{t("reports.repeated", { count: report.repeatCount })}</p> : null}
								{report.outputs?.length ? <ul className="mt-2 flex flex-wrap gap-2">{report.outputs.map((output, outputIndex) => <li key={`${output.kind}-${output.reference}-${outputIndex}`}>{output.kind === "artifact" && isSafeArtifactPath(output.reference) ? <ReportArtifactLink href={artifactLink(report.sourceProjectId || report.projectId, report.sessionId, output.reference)} hostId={sourceHostId} label={output.label || output.reference} /> : output.kind === "artifact" ? <span className="break-all text-muted-foreground">{output.label || output.reference}</span> : <a className="break-all text-primary underline underline-offset-2 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring" href={output.reference} target="_blank" rel="noreferrer">{output.label || output.reference}</a>}</li>)}</ul> : null}
							</div>
						</li>;
					})}
				</ol>
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


function isSafeArtifactPath(reference: string): boolean {
	return reference
		.split("/")
		.every((segment) => segment.trim() !== "" && segment !== "." && segment !== "..") && !reference.includes("\\");
}
