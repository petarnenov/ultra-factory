import { Fragment, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { AlertTriangle, Folder } from "lucide-react";
import type { RemoteHost } from "../hooks/useRemoteHosts";
import type { WorkspaceSummary } from "../types/workspace";
import { SidebarMenuButton, SidebarMenuItem } from "./ui/sidebar";

type Props = {
	hosts: RemoteHost[];
	workspaces: WorkspaceSummary[];
	failedHostIds?: string[];
	renderProject: (host: RemoteHost, workspace: WorkspaceSummary) => ReactNode;
	onRetry: () => void;
	onAddProject?: (hostId: string) => void;
};

/** Only host-specific chrome lives here; project and session rows are shared. */
export function RemoteHostsSection({ hosts, workspaces, failedHostIds = [], renderProject, onRetry, onAddProject }: Props) {
	const { t } = useTranslation();
	return <>
		{hosts.map((host) => {
			if (host.status !== "connected") return <SidebarMenuItem key={host.url} data-host-id={host.hostId}>
				<SidebarMenuButton
					aria-label={host.status === "offline" ? t("remote.retryHost", { label: host.label }) : undefined}
					className="h-9 gap-2 rounded-lg px-2.5 text-sm text-muted-foreground hover:bg-interactive-hover [&_svg]:size-icon-md"
					disabled={host.status === "connecting"}
					onClick={onRetry}
					title={host.status === "offline" ? t(host.failureReason === "incompatible" ? "remote.hostIncompatible" : "remote.hostOffline") : undefined}
				>
					{host.status === "offline" ? <AlertTriangle aria-hidden="true" /> : <Folder aria-hidden="true" />}
					<span className="truncate">{host.label}</span>
					<span className="ml-auto text-xs">{host.status === "connecting" ? t("terminal.connecting") : host.failureReason === "unauthorized" ? t("remote.passwordRejected") : host.failureReason === "incompatible" ? t("remote.updateAO") : t("remote.retryHost", { label: host.label })}</span>
				</SidebarMenuButton>
			</SidebarMenuItem>;
			const projects = workspaces.filter((workspace) => workspace.hostId === host.hostId);
			// A connected host is otherwise visible only through its projects, so an
			// empty one gets the host's own row as its "Add project on" entry point.
			if (projects.length === 0 && onAddProject && !failedHostIds.includes(host.hostId)) return <SidebarMenuItem key={host.url} data-host-id={host.hostId}>
				<SidebarMenuButton
					aria-label={t("remote.addProjectOn", { label: host.label, defaultValue: "Add project on {{label}}" })}
					className="h-9 gap-2 rounded-lg px-2.5 text-sm text-muted-foreground hover:bg-interactive-hover [&_svg]:size-icon-md"
					onClick={() => onAddProject(host.hostId)}
				>
					<Folder aria-hidden="true" />
					<span className="truncate">{host.label}</span>
					<span className="ml-auto text-xs">{t("remote.noProjects", { defaultValue: "No projects" })}</span>
				</SidebarMenuButton>
			</SidebarMenuItem>;
			return <Fragment key={host.url}>
				{projects.map((workspace) => <Fragment key={`${host.hostId}:${workspace.id}`}>{renderProject(host, workspace)}</Fragment>)}
				{failedHostIds.includes(host.hostId) && <SidebarMenuItem>
					<SidebarMenuButton className="h-8 rounded-lg px-2.5 text-left text-xs text-destructive hover:bg-interactive-hover" onClick={onRetry}>{t("remoteHosts.loadFailed")}</SidebarMenuButton>
				</SidebarMenuItem>}
			</Fragment>;
		})}
	</>;
}
