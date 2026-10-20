import { InspectorSection, inspectorEmptyClass } from "@aoagents/product-ui";
import { useNavigate } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { useCoordinatorRoster } from "../hooks/useCoordinatorRoster";
import type { CoordinatorRosterEntry } from "../lib/coordinators";
import { isCoordinatorSession, type WorkspaceSession } from "../types/workspace";

/**
 * Ownership facts from the daemon roster for a local coordinator or worker:
 * who owns a worker, and a coordinator's scope, workers, and latest summary.
 * Every value is read from the daemon; nothing is decided here.
 */
export function SessionOwnershipSection({ session, hostId }: { session: WorkspaceSession; hostId?: string }) {
	const { t } = useTranslation();
	const query = useCoordinatorRoster(session.workspaceId, hostId);
	const roster = query.data;
	const coordinator = isCoordinatorSession(session);
	const entry = roster?.coordinators.find((candidate) =>
		candidate.coordinatorSessionId === (coordinator ? session.id : session.ownerSessionId),
	);
	const title = coordinator && entry
		? t("inspector.workersCount", { count: entry.workerCount })
		: t("ownership.section");
	return (
		<InspectorSection surface={false} title={title}>
			<div className="flex flex-col gap-1.5" data-testid="session-ownership">
				{query.isLoading ? (
					<p className={inspectorEmptyClass}>{t("inspector.workersLoading")}</p>
				) : roster === undefined ? (
					<p className={inspectorEmptyClass} role="status">
						{t("inspector.workersUnavailable")}
					</p>
				) : coordinator ? (
					entry ? <CoordinatorFacts entry={entry} projectId={session.workspaceId} /> : null
				) : (
					<WorkerOwner
						entry={entry}
						ownerSessionId={session.ownerSessionId ?? null}
						rootSessionId={roster.rootSessionId}
					/>
				)}
			</div>
		</InspectorSection>
	);
}

function WorkerOwner({
	entry,
	ownerSessionId,
	rootSessionId,
}: {
	entry: CoordinatorRosterEntry | undefined;
	ownerSessionId: string | null;
	rootSessionId: string | null;
}) {
	const { t } = useTranslation();
	let owner: string;
	if (ownerSessionId !== null) {
		owner = t("ownership.coordinatorOwner", { id: ownerSessionId, scope: entry?.coordinatorScope ?? "" });
	} else if (rootSessionId !== null) {
		owner = t("ownership.rootOwned", { root: rootSessionId });
	} else {
		owner = t("ownership.unassigned");
	}
	return (
		<>
			<p className="text-sm" data-testid="session-owner">
				<span className="text-settings-muted">{t("ownership.owner")}: </span>
				{owner}
			</p>
			{entry?.needsRecovery ? <RecoveryNotice message={t("ownership.ownerNeedsRecovery")} /> : null}
		</>
	);
}

function CoordinatorFacts({ entry, projectId }: { entry: CoordinatorRosterEntry; projectId: string }) {
	const { t } = useTranslation();
	const navigate = useNavigate();
	const summary = entry.latestSummary;
	return (
		<>
			<p className="text-sm">
				<span className="text-settings-muted">{t("ownership.scope")}: </span>
				{entry.coordinatorScope}
			</p>
			{entry.needsRecovery ? <RecoveryNotice message={t("ownership.needsRecovery")} /> : null}
			{entry.workerSessionIds.length === 0 ? (
				<p className={inspectorEmptyClass}>{t("ownership.noWorkers")}</p>
			) : (
				entry.workerSessionIds.map((workerId) => (
					<button
						className="w-full cursor-pointer truncate rounded-lg bg-settings-row px-3 py-1.5 text-left text-sm"
						data-testid="coordinator-worker-row"
						key={workerId}
						onClick={() =>
							void navigate({
								to: "/projects/$projectId/sessions/$sessionId",
								params: { projectId, sessionId: workerId },
							})
						}
						type="button"
					>
						{workerId}
					</button>
				))
			)}
			{summary ? (
				<div className="text-xs leading-normal" data-testid="coordinator-latest-summary">
					<p className="text-settings-muted">{t("ownership.latestSummary")}</p>
					<p>
						{summary.state}
						{summary.result ? ` · ${summary.result}` : ""}
					</p>
					{summary.escalation ? (
						<p className="text-warning">
							{t("ownership.escalation", { reason: summary.escalationReason ?? "" })}
						</p>
					) : null}
				</div>
			) : null}
		</>
	);
}

function RecoveryNotice({ message }: { message: string }) {
	return (
		<p className="text-xs leading-normal text-warning" data-testid="ownership-needs-recovery" role="alert">
			{message}
		</p>
	);
}
