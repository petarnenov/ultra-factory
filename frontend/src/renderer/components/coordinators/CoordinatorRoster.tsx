import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { useCoordinatorRoster } from "../../hooks/useCoordinatorRoster";
import { useRemoteHosts } from "../../hooks/useRemoteHosts";
import { useRemoteProjectQuery, workspaceQueryOptions } from "../../hooks/useWorkspaceQuery";
import { apiErrorMessage } from "../../lib/api-client";
import {
	COORDINATOR_DEFAULT_LIMITS,
	type CoordinatorRoster,
	type CoordinatorRosterEntry,
	coordinatorErrorKey,
	coordinatorRosterQueryKey,
	type ProjectConfig,
	parseLimit,
	reassignSessionOwner,
	setCoordinatorLimitsOnBehalfOfRoot,
	stopSession,
} from "../../lib/coordinators";
import type { WorkspaceSession } from "../../types/workspace";
import { ConfirmDialog } from "../ConfirmDialog";
import { Button } from "../ui/button";
import { Input } from "../ui/input";
import { Label } from "../ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "../ui/select";
import { StartCoordinatorDialog } from "./StartCoordinatorDialog";

const ROOT_TARGET = "__root__";

/** Readable text for a daemon refusal, else the daemon's own text. */
function useErrorMessage() {
	const { t } = useTranslation();
	return (error: unknown, fallback: string): string => {
		const key = coordinatorErrorKey(error);
		return key ? String(t(key)) : apiErrorMessage(error, fallback);
	};
}

/** Root, coordinators and workers, with the operator's actions. */
export function CoordinatorRoster({ projectId, hostId, config }: { projectId: string; hostId?: string; config: ProjectConfig }) {
	const { t } = useTranslation();
	const queryClient = useQueryClient();
	const query = useCoordinatorRoster(projectId, hostId);
	const roster = query.data;
	const sessions = useProjectSessions(projectId, hostId);
	const hosts = useRemoteHosts().hosts;
	const [startOpen, setStartOpen] = useState(false);
	const [actionError, setActionError] = useState<string | null>(null);
	const enabled = config.coordinators?.enabled ?? false;
	const refresh = () => void queryClient.invalidateQueries({ queryKey: coordinatorRosterQueryKey(projectId, hostId) });
	const hostLabel = (id: string) => hosts.find((host) => host.hostId === id)?.label ?? id;

	const startDisabledReason = !enabled
		? t("coordinators.roster.startDisabledOff")
		: roster && !roster.rootSessionId
			? t("coordinators.roster.startDisabledNoRoot")
			: roster && roster.capacity.activeCoordinators >= roster.capacity.maxCoordinators
				? t("coordinators.roster.startDisabledCap")
				: null;

	return (
		<section className="flex flex-col gap-4" data-testid="coordinator-roster">
			<div className="flex items-center justify-between gap-4">
				<h3 className="text-sm font-medium">{t("coordinators.roster.title")}</h3>
				<Button disabled={Boolean(startDisabledReason) || !roster} onClick={() => setStartOpen(true)} size="sm" type="button" variant="outline">
					{t("coordinators.roster.start")}
				</Button>
			</div>
			{startDisabledReason ? <p className="text-sm text-settings-muted">{startDisabledReason}</p> : null}
			{query.isLoading ? <p className="text-sm text-settings-muted">{t("coordinators.roster.loading")}</p> : null}
			{!query.isLoading && roster === undefined ? (
				<p className="text-sm text-error" role="alert">{t("coordinators.roster.loadFailed")}</p>
			) : null}
			{roster ? (
				<>
					<div className="flex flex-col gap-1 text-sm">
						<p>
							<span className="text-settings-muted">{t("coordinators.roster.root")}: </span>
							{roster.rootSessionId ?? t("coordinators.roster.noRoot")}
						</p>
						<p className="text-settings-muted" data-testid="roster-capacity">
							{t("coordinators.roster.capacity", {
								coordinators: `${roster.capacity.activeCoordinators}/${roster.capacity.maxCoordinators}`,
								workers: `${roster.capacity.activeWorkers}/${roster.capacity.maxWorkers}`,
							})}
						</p>
						{roster.rootWorkers.length > 0 || roster.rootWorkerSessionIds.length > 0 ? (
							<div className="text-settings-muted">
								<p>{t("coordinators.roster.rootWorkers", { count: roster.rootWorkers.length || roster.rootWorkerSessionIds.length })}</p>
								<ul className="pl-4" data-testid="root-worker-list">
									{(roster.rootWorkers.length > 0 ? roster.rootWorkers : roster.rootWorkerSessionIds.map((sessionId) => ({
										sessionId, agentProfileId: null, remote: false, hostId: "", hostLabel: "",
									}))).map((worker) => (
										<li key={`${worker.remote ? worker.hostId : "local"}:${worker.sessionId}`}>
											{worker.remote ? `${worker.sessionId} on ${worker.hostLabel || hostLabel(worker.hostId)}` : worker.sessionId}
										</li>
									))}
								</ul>
							</div>
						) : null}
					</div>
					{roster.coordinators.length === 0 ? (
						<p className="text-sm text-settings-muted">{t("coordinators.roster.none")}</p>
					) : (
						<ul className="flex flex-col gap-4">
							{roster.coordinators.map((entry) => (
								<CoordinatorItem
									config={config}
									entry={entry}
									hostId={hostId}
									hostLabel={hostLabel}
									key={entry.coordinatorSessionId}
									onChanged={refresh}
									onError={setActionError}
									roster={roster}
									session={sessions.get(entry.coordinatorSessionId)}
									sessions={sessions}
								/>
							))}
						</ul>
					)}
					{actionError ? <p className="text-sm text-error" role="alert">{actionError}</p> : null}
				</>
			) : null}
			{startOpen && roster ? (
				<StartCoordinatorDialog
					config={config}
					hostId={hostId}
					onOpenChange={setStartOpen}
					onStarted={refresh}
					open={startOpen}
					projectId={projectId}
					roster={roster}
				/>
			) : null}
		</section>
	);
}

function CoordinatorItem({
	entry,
	session,
	sessions,
	roster,
	config,
	hostId,
	hostLabel,
	onChanged,
	onError,
}: {
	entry: CoordinatorRosterEntry;
	session: WorkspaceSession | undefined;
	sessions: Map<string, WorkspaceSession>;
	roster: CoordinatorRoster;
	config: ProjectConfig;
	hostId?: string;
	hostLabel: (id: string) => string;
	onChanged: () => void;
	onError: (message: string | null) => void;
}) {
	const { t } = useTranslation();
	const [editing, setEditing] = useState(false);
	const [stopping, setStopping] = useState(false);
	const summary = entry.latestSummary;
	const agent = [session?.provider, session?.model, session?.effort].filter(Boolean).join(" · ");
	const targets = roster.coordinators.filter((other) => other.coordinatorSessionId !== entry.coordinatorSessionId);
	return (
		<li className="flex flex-col gap-2 pl-4" data-testid="coordinator-row">
			<div className="flex items-start justify-between gap-3">
				<div className="flex min-w-0 flex-col gap-0.5">
					<span className="truncate text-sm font-medium">{entry.coordinatorScope}</span>
					<span className="truncate font-mono text-xs text-settings-muted">{entry.coordinatorSessionId}</span>
					{agent ? <span className="text-xs text-settings-muted">{agent}</span> : null}
				</div>
				<div className="flex shrink-0 gap-1">
					<Button onClick={() => setEditing(true)} size="sm" type="button" variant="ghost">
						{t("coordinators.roster.editLimits")}
					</Button>
					<Button onClick={() => setStopping(true)} size="sm" type="button" variant="ghost">
						{t("coordinators.roster.stop")}
					</Button>
				</div>
			</div>
			<p className="text-sm" data-testid="coordinator-limit">
				<span className="text-settings-muted">{t("coordinators.roster.workers")}: </span>
				{t("coordinators.roster.used", { used: entry.activeWorkerCount, allowed: entry.effectiveWorkerLimit })}
				{entry.hostSlots.map((slot) => (
					<span className="ml-3 text-settings-muted" key={slot.hostId}>
						{t("coordinators.roster.hostSlot", { host: hostLabel(slot.hostId), used: slot.used, slots: slot.slots })}
					</span>
				))}
			</p>
			{entry.needsRecovery ? <p className="text-xs text-warning" role="alert">{t("coordinators.roster.needsRecovery")}</p> : null}
			{summary ? (
				<p className="text-xs text-settings-muted">
					{t("coordinators.roster.summary")}: {summary.state}
					{summary.result ? ` · ${summary.result}` : ""}
				</p>
			) : null}
			{entry.workers.length === 0 ? (
				<p className="text-xs text-settings-muted">{t("coordinators.roster.noWorkers")}</p>
			) : (
				entry.workers.map((worker) => worker.remote ? (
					<p className="text-xs text-settings-muted" data-testid="coordinator-remote-worker-row" key={`${worker.hostId}:${worker.sessionId}`}>
						{worker.sessionId} on {worker.hostLabel || worker.hostId}
					</p>
				) : (
					<WorkerRow
						hostId={hostId}
						key={worker.sessionId}
						onChanged={onChanged}
						onError={onError}
						profileId={worker.agentProfileId}
						rootId={roster.rootSessionId}
						sessionId={worker.sessionId}
						sessions={sessions}
						targets={targets}
					/>
				))
			)}
			{editing ? (
				<EditLimitsDialog
					config={config}
					entry={entry}
					hostId={hostId}
					onClose={() => setEditing(false)}
					onSaved={() => {
						setEditing(false);
						onChanged();
					}}
				/>
			) : null}
			{stopping ? (
				<StopDialog
					entry={entry}
					hostId={hostId}
					onClose={() => setStopping(false)}
					onStopped={() => {
						setStopping(false);
						onChanged();
					}}
				/>
			) : null}
		</li>
	);
}

function WorkerRow({
	sessionId,
	profileId,
	rootId,
	targets,
	hostId,
	sessions,
	onChanged,
	onError,
}: {
	sessionId: string;
	profileId: string | null;
	rootId: string | null;
	targets: CoordinatorRosterEntry[];
	hostId?: string;
	sessions: Map<string, WorkspaceSession>;
	onChanged: () => void;
	onError: (message: string | null) => void;
}) {
	const { t } = useTranslation();
	const fail = useErrorMessage();
	const [busy, setBusy] = useState(false);
	const worker = sessions.get(sessionId);
	const reassign = async (target: string) => {
		if (!rootId) return;
		setBusy(true);
		onError(null);
		try {
			await reassignSessionOwner(sessionId, target === ROOT_TARGET ? null : target, rootId, hostId);
			onChanged();
		} catch (error) {
			onError(fail(error, t("coordinators.errors.reassignFailed")));
		} finally {
			setBusy(false);
		}
	};
	return (
		<div className="flex items-center justify-between gap-3 text-sm">
			<div className="flex min-w-0 flex-col">
				<span className="truncate font-mono text-xs">{sessionId}</span>
				<span className="truncate text-xs text-settings-muted">
					{[profileId, worker?.provider, worker?.model].filter(Boolean).join(" · ") || t("coordinators.roster.noProfile")}
				</span>
			</div>
			<Select disabled={busy || !rootId} onValueChange={(value) => void reassign(value)} value="">
				<SelectTrigger aria-label={t("coordinators.roster.reassignTo")} className="w-44 shrink-0">
					<SelectValue placeholder={t("coordinators.roster.reassignTo")} />
				</SelectTrigger>
				<SelectContent>
					<SelectItem value={ROOT_TARGET}>{t("coordinators.roster.rootOption")}</SelectItem>
					{targets.map((target) => (
						<SelectItem key={target.coordinatorSessionId} value={target.coordinatorSessionId}>
							{t("coordinators.roster.coordinatorOption", { scope: target.coordinatorScope })}
						</SelectItem>
					))}
				</SelectContent>
			</Select>
		</div>
	);
}

function EditLimitsDialog({
	entry,
	config,
	hostId,
	onClose,
	onSaved,
}: {
	entry: CoordinatorRosterEntry;
	config: ProjectConfig;
	hostId?: string;
	onClose: () => void;
	onSaved: () => void;
}) {
	const { t } = useTranslation();
	const fail = useErrorMessage();
	const perCoordinator = config.coordinators?.maxWorkersPerCoordinator || COORDINATOR_DEFAULT_LIMITS.workersPerCoordinator;
	const [limit, setLimit] = useState(String(entry.effectiveWorkerLimit));
	const [busy, setBusy] = useState(false);
	const [error, setError] = useState<string | null>(null);
	const parsed = parseLimit(limit, perCoordinator);
	const invalid = typeof parsed !== "number" || parsed < 1;
	const belowUsage = typeof parsed === "number" && parsed < entry.activeWorkerCount;
	const save = async () => {
		if (typeof parsed !== "number") return;
		setBusy(true);
		setError(null);
		try {
			await setCoordinatorLimitsOnBehalfOfRoot(
				entry.coordinatorSessionId,
				{ workerLimit: parsed, hostSlots: entry.hostSlots.map((slot) => ({ hostId: slot.hostId, slots: slot.slots })) },
				hostId,
			);
			onSaved();
		} catch (failure) {
			setError(fail(failure, t("coordinators.errors.limitsFailed")));
		} finally {
			setBusy(false);
		}
	};
	return (
		<ConfirmDialog
			busy={busy}
			confirmDisabled={invalid}
			confirmLabel={t("coordinators.edit.save")}
			description={t("coordinators.edit.description", { scope: entry.coordinatorScope })}
			error={error}
			onConfirm={() => void save()}
			onOpenChange={(open) => !open && onClose()}
			open
			title={t("coordinators.edit.title")}
		>
			<div className="flex flex-col gap-2">
				<Label htmlFor="edit-limit">{t("coordinators.start.workerLimit")}</Label>
				<Input className="w-24" id="edit-limit" inputMode="numeric" onChange={(event) => setLimit(event.target.value)} value={limit} />
				{invalid ? <p className="text-sm text-error">{t("coordinators.start.errors.workerLimitRange")}</p> : null}
				{belowUsage ? <p className="text-sm text-warning">{t("coordinators.edit.keepWorkers", { count: entry.activeWorkerCount })}</p> : null}
			</div>
		</ConfirmDialog>
	);
}

function StopDialog({
	entry,
	hostId,
	onClose,
	onStopped,
}: {
	entry: CoordinatorRosterEntry;
	hostId?: string;
	onClose: () => void;
	onStopped: () => void;
}) {
	const { t } = useTranslation();
	const fail = useErrorMessage();
	const [busy, setBusy] = useState(false);
	const [error, setError] = useState<string | null>(null);
	const owned = entry.workerCount;
	const stop = async () => {
		setBusy(true);
		setError(null);
		try {
			await stopSession(entry.coordinatorSessionId, hostId);
			onStopped();
		} catch (failure) {
			setError(fail(failure, t("coordinators.errors.stopFailed")));
		} finally {
			setBusy(false);
		}
	};
	return (
		<ConfirmDialog
			busy={busy}
			confirmDisabled={owned > 0}
			confirmLabel={t("coordinators.stop.confirm")}
			description={t("coordinators.stop.description", { scope: entry.coordinatorScope })}
			destructive
			error={error}
			onConfirm={() => void stop()}
			onOpenChange={(open) => !open && onClose()}
			open
			title={t("coordinators.stop.title")}
		>
			{owned > 0 ? <p className="text-sm text-warning">{t("coordinators.stop.ownsWorkers", { count: owned })}</p> : null}
		</ConfirmDialog>
	);
}

/** Sessions by id, for each coordinator's agent and worker details. */
function useProjectSessions(projectId: string, hostId?: string) {
	const workspaceQuery = useQuery({ ...workspaceQueryOptions, enabled: !hostId });
	const remoteProjectQuery = useRemoteProjectQuery(hostId ?? "", projectId);
	const workspace = hostId ? remoteProjectQuery.data : workspaceQuery.data?.find((item) => item.id === projectId);
	return new Map((workspace?.sessions ?? []).map((session) => [session.id, session]));
}
