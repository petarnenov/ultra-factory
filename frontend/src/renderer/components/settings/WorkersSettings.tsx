import { Clock, LoaderCircle, Server } from "lucide-react";
import type { ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { useHubHosts, useHubHostSessionCounts, type HubHost, type HubHostStatus, type HubOrphan } from "../../hooks/useHubHosts";
import { AddWorkerDialog } from "./AddWorkerDialog";
import { Badge } from "../ui/badge";
import { Button } from "../ui/button";
import { Tooltip, TooltipContent, TooltipTrigger } from "../ui/tooltip";
import { SettingsRow } from "./SettingsRow";
import { SettingsSection } from "./SettingsSection";

type BadgeVariant = "neutral" | "success" | "warning" | "error";

const statusLabel = {
	draining: "settings.workers.status.draining",
	unknown: "settings.workers.status.unknown",
	provisioning: "settings.workers.status.provisioning",
	online: "settings.workers.status.online",
	unreachable: "settings.workers.status.unreachable",
	auth_failed: "settings.workers.status.auth_failed",
	removing: "settings.workers.status.removing",
	failed: "settings.workers.status.failed",
} as const satisfies Record<HubHostStatus, string>;

const statusVariant: Record<HubHostStatus, BadgeVariant> = {
	draining: "warning",
	unknown: "neutral",
	provisioning: "neutral",
	online: "success",
	unreachable: "warning",
	auth_failed: "error",
	removing: "neutral",
	failed: "error",
};

export function WorkerStatusBadge({ host }: { host: Pick<HubHost, "status" | "lastError"> }) {
	const { t } = useTranslation();
	const busy = host.status === "provisioning" || host.status === "removing";
	const badge = (
		<Badge data-status={host.status} tabIndex={host.status === "failed" && host.lastError ? 0 : undefined} variant={statusVariant[host.status] ?? "neutral"}>
			{busy ? <LoaderCircle aria-hidden="true" className="animate-spin" /> : null}
			{t(statusLabel[host.status] ?? statusLabel.unknown, { defaultValue: host.status === "draining" ? "Draining" : "Unknown" })}
		</Badge>
	);
	if (host.status !== "failed" || !host.lastError) return badge;
	return (
		<Tooltip>
			<TooltipTrigger asChild>{badge}</TooltipTrigger>
			<TooltipContent>{host.lastError}</TooltipContent>
		</Tooltip>
	);
}

/** Settings → Workers (web build only): the hub's hosts with live status. The
 * add/remove dialogs plug in through `addWorkerAction` and `rowActions`. */
export function WorkersSettings({
	titleHidden,
	addWorkerAction,
	rowActions,
	onOpenHost,
}: {
	titleHidden?: boolean;
	addWorkerAction?: ReactNode;
	rowActions?: (host: HubHost) => ReactNode;
	onOpenHost?: (host: HubHost) => void;
}) {
	const { t, i18n } = useTranslation();
	const { data, error, isPending, refetch } = useHubHosts();
	const workers = data?.workers ?? [];
	const sessionCounts = useHubHostSessionCounts(workers);
	const idleIds = new Set((data?.idle ?? []).map((worker) => worker.id));
	const dateFormat = new Intl.DateTimeFormat(i18n.language, { dateStyle: "medium", timeStyle: "short" });

	return <>
		<SettingsSection title={t("settings.workers")} sectionId="workers" titleHidden={titleHidden} grouped>
			<div className="flex justify-end px-3 py-1">{addWorkerAction ?? <AddWorkerDialog />}</div>
			{isPending ? <p className="px-3 py-2 text-xs text-settings-muted" role="status">{t("settings.workers.loading")}</p> : null}
			{!isPending && !error && workers.length === 0 ? <p className="px-3 py-2 text-pretty text-xs text-settings-muted">{data?.providerConfigured === false ? t("settings.workers.providerUnavailable") : t("settings.workers.empty")}</p> : null}
			{workers.map((host) => {
				const sessions = sessionCounts[host.id];
				const description = [
					t(host.source === "railway" ? "settings.workers.source.railway" : "settings.workers.source.manual"),
					t("settings.workers.createdAt", { date: formatDate(dateFormat, host.createdAt) }),
				].join(" · ");
				return (
					<div data-testid="worker-row" data-worker-id={host.id} key={host.id}>
						<SettingsRow icon={Server} label={host.label} description={description}>
							<div className="flex items-center gap-2">
								{idleIds.has(host.id) && data?.idleStopHours !== undefined ? <IdleWarning hours={data.idleStopHours} /> : null}
								{sessions !== undefined ? <span className="text-xs tabular-nums text-settings-muted">{t("settings.workers.activeSessions", { count: sessions })}</span> : null}
								<WorkerStatusBadge host={host} />
								{onOpenHost && (host.status === "online" || host.status === "draining") ? <Button onClick={() => onOpenHost(host)} size="sm" type="button" variant="outline">{t("settings.workers.open")}</Button> : null}
								{rowActions?.(host)}
							</div>
						</SettingsRow>
					</div>
				);
			})}
			{error ? <div className="flex items-center gap-2 px-3 py-2 text-xs text-error" role="alert">{error.message}<Button onClick={() => void refetch()} size="sm" type="button" variant="outline">{t("notify.retry")}</Button></div> : null}
		</SettingsSection>
		{data && data.orphans.length > 0 ? <OrphansSection orphans={data.orphans} /> : null}
	</>;
}

// The BFF only reports idleness (AO_HUB_IDLE_STOP_HOURS); stopping a Railway
// worker without losing its volume is not verified, so the operator decides.
function IdleWarning({ hours }: { hours: number }) {
	const { t } = useTranslation();
	return (
		<Tooltip>
			<TooltipTrigger asChild>
				<span className="inline-flex items-center gap-1 text-xs text-warning" data-testid="worker-idle" tabIndex={0}>
					<Clock aria-hidden="true" className="size-3" />
					{t("settings.workers.idle", { hours })}
				</span>
			</TooltipTrigger>
			<TooltipContent>{t("settings.workers.idleHint", { hours })}</TooltipContent>
		</Tooltip>
	);
}

// Adopt and Delete need the orphan's volume id and credentials, which the
// Railway services query does not return, so both stay disabled.
function OrphansSection({ orphans }: { orphans: HubOrphan[] }) {
	const { t } = useTranslation();
	return (
		<SettingsSection title={t("settings.workers.orphans")} sectionId="workerOrphans" grouped>
			<p className="px-3 text-pretty text-xs leading-relaxed text-settings-muted">{t("settings.workers.orphansUnavailable")}</p>
			{orphans.map((orphan) => (
				<div data-testid="worker-orphan" key={orphan.id}>
					<SettingsRow icon={Server} label={orphan.name} description={orphan.id}>
						<div className="flex items-center gap-1">
							<Button disabled size="sm" type="button" variant="outline">{t("settings.workers.adopt")}</Button>
							<Button disabled size="sm" type="button" variant="outline">{t("settings.workers.delete")}</Button>
						</div>
					</SettingsRow>
				</div>
			))}
		</SettingsSection>
	);
}

function formatDate(format: Intl.DateTimeFormat, value: string): string {
	const date = new Date(value);
	return Number.isNaN(date.getTime()) ? value : format.format(date);
}
