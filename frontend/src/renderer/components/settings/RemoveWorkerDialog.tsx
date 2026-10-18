import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { hubBaseUrl, hubHostsQueryKey, type HubHost, type HubWorkers } from "../../hooks/useHubHosts";
import { isWebBuild } from "../../lib/platform";
import { webHostBaseUrl } from "../../lib/web-remotes";
import { ConfirmDialog } from "../ConfirmDialog";
import { Button } from "../ui/button";
import { Checkbox } from "../ui/checkbox";
import { Input } from "../ui/input";
import { Label } from "../ui/label";

/** A non-terminated session as the worker (or the BFF's 409 body). */
export type WorkerSession = { id: string; displayName?: string; activity?: { state?: string } };

type DeleteError = { error?: string; message?: string; sessions?: WorkerSession[]; worker?: HubHost };

// Mirrors the BFF's removal guard (workers-api.ts nonTerminatedSessions): only
// sessions that are not terminated block a non-forced delete.
async function fetchWorkerSessions(hostId: string, signal: AbortSignal): Promise<WorkerSession[]> {
	const response = await fetch(`${webHostBaseUrl(hubBaseUrl(), hostId)}/api/v1/sessions?active=true`, { cache: "no-store", credentials: "same-origin", signal });
	if (!response.ok) throw new Error(`Could not load sessions (HTTP ${response.status})`);
	const body = (await response.json()) as { sessions?: Array<WorkerSession & { isTerminated?: unknown }> };
	return (body.sessions ?? []).filter((session) => session.isTerminated === false);
}

/** "Remove" row action (web): a Railway worker is deleted with its service,
 * a manual host only loses its registry record. */
export function RemoveWorkerDialog({ host }: { host: HubHost }) {
	const { t } = useTranslation();
	const queryClient = useQueryClient();
	const [open, setOpen] = useState(false);
	const [confirmText, setConfirmText] = useState("");
	const [force, setForce] = useState(false);
	const [busy, setBusy] = useState(false);
	const [error, setError] = useState<string | null>(null);
	const [blockingSessions, setBlockingSessions] = useState<WorkerSession[] | null>(null);
	const railway = host.source === "railway";

	const sessionsQuery = useQuery({
		queryKey: ["hub", "host-session-list", host.id] as const,
		enabled: isWebBuild && open && railway,
		queryFn: ({ signal }) => fetchWorkerSessions(host.id, signal),
		staleTime: 0,
		retry: false,
	});
	const sessions = blockingSessions ?? sessionsQuery.data;

	// The dialog stays mounted while its own delete marks the row as removing.
	if (!host.removable || (host.status === "removing" && !open)) return null;

	function reset() {
		setConfirmText("");
		setForce(false);
		setError(null);
		setBlockingSessions(null);
	}

	function patchWorkers(update: (workers: HubHost[]) => HubHost[]) {
		queryClient.setQueryData<HubWorkers>(hubHostsQueryKey, (data) => (data ? { ...data, workers: update(data.workers) } : data));
	}

	async function submit() {
		setBusy(true);
		setError(null);
		const path = railway ? `/hub/workers/${encodeURIComponent(host.id)}${force ? "?force=1" : ""}` : `/hub/hosts/${encodeURIComponent(host.id)}`;
		if (railway) patchWorkers((workers) => workers.map((worker) => (worker.id === host.id ? { ...worker, status: "removing", lastError: undefined } : worker)));
		try {
			const response = await fetch(`${hubBaseUrl()}${path}`, { method: "DELETE", credentials: "same-origin" });
			// 404: the record is already gone, which is what the operator asked for.
			if (response.ok || response.status === 404) {
				patchWorkers((workers) => workers.filter((worker) => worker.id !== host.id));
				setBusy(false);
				setOpen(false);
				reset();
				void queryClient.invalidateQueries({ queryKey: hubHostsQueryKey });
				return;
			}
			const body = ((await response.json().catch(() => null)) as DeleteError | null) ?? {};
			if (body.worker) {
				const worker = body.worker;
				patchWorkers((workers) => workers.map((entry) => (entry.id === worker.id ? worker : entry)));
			}
			if (body.error === "active_sessions") {
				setBlockingSessions(body.sessions ?? []);
				setError(t("settings.workers.remove.activeSessionsError"));
			} else if (body.error === "session_check_failed") {
				setError(t("settings.workers.remove.sessionCheckFailed"));
			} else if (body.error === "worker_still_provisioning") {
				setError(t("settings.workers.remove.stillProvisioning"));
			} else if (body.error === "worker_delete_failed") {
				setError(t("settings.workers.remove.railwayRefused"));
			} else {
				setError(body.message || body.error || t("settings.workers.remove.failed", { status: response.status }));
			}
		} catch (err) {
			setError(err instanceof Error ? err.message : t("settings.workers.remove.failed", { status: 0 }));
		}
		setBusy(false);
		void queryClient.invalidateQueries({ queryKey: hubHostsQueryKey });
	}

	return <>
		<Button onClick={() => setOpen(true)} size="sm" type="button" variant="outline">{t("settings.workers.remove")}</Button>
		<ConfirmDialog
			open={open}
			onOpenChange={(next) => { setOpen(next); if (!next) reset(); }}
			title={t("settings.workers.remove.title", { label: host.label })}
			description={t(railway ? "settings.workers.remove.railwayWarning" : "settings.workers.remove.manualNotice")}
			confirmLabel={t(railway ? "settings.workers.remove.confirm" : "settings.workers.remove.confirmManual")}
			destructive
			busy={busy}
			confirmDisabled={railway && confirmText !== host.label}
			error={error}
			onConfirm={() => void submit()}
		>
			{railway ? <>
				<div className="flex flex-col gap-1" data-testid="worker-sessions">
					<p className="text-xs font-medium text-settings-label">{t("settings.workers.remove.sessions")}</p>
					{sessions === undefined && sessionsQuery.isPending ? <p className="text-xs text-settings-muted" role="status">{t("settings.workers.remove.sessionsLoading")}</p> : null}
					{sessions === undefined && sessionsQuery.isError ? <p className="text-pretty text-xs text-settings-muted">{t("settings.workers.remove.sessionsUnavailable")}</p> : null}
					{sessions?.length === 0 ? <p className="text-xs text-settings-muted">{t("settings.workers.remove.noSessions")}</p> : null}
					{sessions && sessions.length > 0 ? (
						<ul className="flex flex-col gap-0.5 text-xs text-settings-label">
							{sessions.map((session) => (
								<li className="flex justify-between gap-2" key={session.id}>
									<span className="truncate">{session.displayName || session.id}</span>
									{session.activity?.state ? <span className="shrink-0 text-settings-muted">{session.activity.state}</span> : null}
								</li>
							))}
						</ul>
					) : null}
				</div>
				<div className="flex flex-col gap-1.5">
					<Label htmlFor={`remove-worker-${host.id}`}>{t("settings.workers.remove.typeLabel", { label: host.label })}</Label>
					<Input autoComplete="off" disabled={busy} id={`remove-worker-${host.id}`} onChange={(event) => setConfirmText(event.target.value)} spellCheck={false} value={confirmText} />
				</div>
				<div className="flex items-center gap-2">
					<Checkbox checked={force} disabled={busy} id={`remove-worker-force-${host.id}`} onCheckedChange={(checked) => setForce(checked === true)} />
					<Label htmlFor={`remove-worker-force-${host.id}`}>{t("settings.workers.remove.force")}</Label>
				</div>
			</> : null}
		</ConfirmDialog>
	</>;
}
