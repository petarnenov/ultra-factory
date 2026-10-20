import { useNavigate } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { SessionsBoardGridView } from "@aoagents/product-ui";
import { LayoutDashboard } from "lucide-react";
import { useRemoteWorkspaces, useWorkspaceQuery, useProjectIdentity } from "../hooks/useWorkspaceQuery";
import { useRemoteHosts } from "../hooks/useRemoteHosts";
import { labelForHost } from "../lib/host-clients";
import { LOCAL_HOST, refKey, sessionUiKey } from "../lib/hosts";
import { boardKanbanColumnOrder, getKanbanColumnView } from "../lib/session-presentation";
import { workerSessions, type WorkspaceSession } from "../types/workspace";
import { agentLabel } from "../lib/agent-options";
import { sessionsBoardLabels, BoardSessionCardAdapter } from "./SessionsBoardAdapters";

export function AggregateProjectBoard({ projectId }: { projectId: string }) {
	const { t } = useTranslation();
	const navigate = useNavigate();
	const localQuery = useWorkspaceQuery();
	const { hosts } = useRemoteHosts();
	const localProject = localQuery.data?.find((workspace) => workspace.id === projectId);
	const localIdentity = useProjectIdentity(undefined, projectId);
	const associations = localIdentity.data?.aggregateProjects ?? [];
	// Same projects/sessions queries as the sidebar, so one fetch per host serves both.
	const remote = useRemoteWorkspaces();
	const associated = associations.map((ref) => {
		return {
			hostId: ref.hostId,
			projectId: ref.projectId,
			project: remote.data.find((workspace) => workspace.hostId === ref.hostId && workspace.id === ref.projectId),
			host: hosts.find((host) => host.hostId === ref.hostId),
			loaded: remote.loadedProjectHostIds.includes(ref.hostId) && remote.loadedSessionHostIds.includes(ref.hostId),
			failed: remote.failedHostIds.includes(ref.hostId),
		};
	});
	const sessions: WorkspaceSession[] = [
		...(localProject ? workerSessions(localProject.sessions).map((session) => ({ ...session, hostId: LOCAL_HOST })) : []),
		...associated.flatMap((entry) => {
			return entry.project ? workerSessions(entry.project.sessions).map((session) => ({ ...session, hostId: entry.hostId })) : [];
		}),
	].filter((session) => !session.isTerminated && session.status !== "terminated" && session.kanbanColumn !== "archive");
	const partial = associated.filter((entry) => !entry.host || entry.host.status !== "connected" || entry.failed || (entry.loaded && !entry.project));
	const loading = localQuery.isPending || localIdentity.isPending || associated.some((entry) => entry.host?.status === "connected" && !entry.loaded && !entry.failed);
	const columns = boardKanbanColumnOrder.map((column) => getKanbanColumnView(column, t));
	const labels = sessionsBoardLabels(t);
	const projectRoute = (hostId: string, targetProjectId: string, sessionId: string) => hostId === LOCAL_HOST
		? navigate({ to: "/projects/$projectId/sessions/$sessionId", params: { projectId: targetProjectId, sessionId } })
		: navigate({ to: "/host/$hostId/project/$projectId/session/$sessionId", params: { hostId, projectId: targetProjectId, sessionId } });
	const localHostName = t("shell.localMachine", { defaultValue: "This machine" });
	return (
		<div className="relative flex h-full min-h-0 flex-col bg-background text-foreground" data-testid="aggregate-project-board" data-project-id={projectId}>
			<div className="flex h-toolbar shrink-0 items-center gap-2 border-b border-border-strong px-4">
				<LayoutDashboard aria-hidden="true" className="size-icon-md" />
				<strong className="truncate">{localProject?.name ?? projectId}</strong>
				<span className="text-muted-foreground">· {t("shell.aggregateKanban")}</span>
			</div>
			<p className="px-4 py-2 text-xs text-muted-foreground">{t("shell.aggregateKanbanDescription")}</p>
			{partial.length ? <div className="border-y border-warning/40 bg-warning/10 px-4 py-2 text-xs" role="status">
				<p>{t("shell.aggregatePartialHosts")}</p>
				{partial.map((entry) => <p key={`${entry.hostId}:${entry.projectId}`}>{t("shell.aggregateUnavailableProject", { host: entry.host?.label || entry.hostId, project: entry.projectId })}</p>)}
			</div> : null}
			{loading ? <p role="status" className="py-10 text-center text-sm text-muted-foreground">{t("shell.aggregateLoading")}</p>
				: localQuery.isError || localIdentity.isError || !localProject ? <p role="alert" className="py-10 text-center text-sm text-destructive">{t("shell.couldNotLoadSessions")}</p>
				: <>
					{associations.length === 0 ? <p role="status" className="px-4 py-2 text-center text-xs text-muted-foreground">{t("shell.aggregateNoRelatedProjects")}</p> : null}
					{sessions.length === 0 ? <p className="py-10 text-center text-sm text-muted-foreground">{partial.length ? t("shell.aggregateNoAvailableSessions") : t("shell.aggregateNoSessions")}</p>
							: <div className="min-h-0 flex-1">
								<SessionsBoardGridView columns={columns} labels={labels} sessions={sessions} sessionKey={(session) => refKey({ host: session.hostId ?? LOCAL_HOST, id: session.id })} renderSessionCard={(session) => {
									const hostId = session.hostId ?? LOCAL_HOST;
									const targetProjectId = hostId === LOCAL_HOST ? projectId : associated.find((entry) => entry.hostId === hostId && entry.projectId === session.workspaceId)?.projectId;
									const hostLabel = hostId === LOCAL_HOST ? localHostName : labelForHost(hostId) ?? hostId;
									return <div key={sessionUiKey(session.id, hostId)} data-session-key={refKey({ host: hostId, id: session.id })}>
										<BoardSessionCardAdapter session={session} sourceLabel={t("shell.aggregateSource", { host: hostLabel, worker: agentLabel(session.provider) })} onOpen={() => targetProjectId && void projectRoute(hostId, targetProjectId, session.id)} />
									</div>;
								}} />
							</div>}
				</>}
		</div>
	);
}
