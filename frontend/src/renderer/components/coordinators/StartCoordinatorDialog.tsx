import { useState } from "react";
import { useTranslation } from "react-i18next";
import { useRemoteHosts } from "../../hooks/useRemoteHosts";
import { apiErrorMessage } from "../../lib/api-client";
import {
	COORDINATOR_HARD_LIMITS,
	COORDINATOR_SCOPE_MAX,
	coordinatorErrorKey,
	coordinatorsFormFrom,
	type CoordinatorRoster,
	type ProjectConfig,
	type SelectionForm,
	type SpawnCoordinatorErrors,
	type SpawnCoordinatorForm,
	spawnCoordinatorOnBehalfOfRoot,
	validateSpawnCoordinator,
} from "../../lib/coordinators";
import { cn } from "@/lib/utils";
import { X } from "lucide-react";
import {
	Dialog,
	DialogClose,
	DialogContent,
	DialogDescription,
	DialogOverlay,
	DialogPortal,
	DialogTitle,
	settingsDialogBodyClass,
	settingsDialogContentClass,
	settingsDialogFooterClass,
	settingsDialogHeaderClass,
} from "../ui/dialog";
import { Button } from "../ui/button";
import { Input } from "../ui/input";
import { Label } from "../ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "../ui/select";
import { AgentSelectionField } from "./AgentSelectionField";
import { COORDINATOR_BRIEF_TEMPLATE } from "./brief-template";

type CoordinatorAgentMode = SpawnCoordinatorForm["coordinatorAgent"];

const emptySelection = (): SelectionForm => ({ mode: "inherit", profileId: "", profileIds: [] });

const initialForm = (): SpawnCoordinatorForm => ({
	scope: "",
	workerLimit: "",
	hostSlots: [{ hostId: "", slots: "1" }],
	coordinatorAgent: "inherit",
	coordinatorProfileId: "",
	customHarness: "",
	customModel: "",
	customEffort: "",
	workerAgent: emptySelection(),
	brief: COORDINATOR_BRIEF_TEMPLATE,
});

/** Starts a coordinator for the live root; the start is audited. */
export function StartCoordinatorDialog({
	open,
	onOpenChange,
	projectId,
	hostId,
	config,
	roster,
	onStarted,
}: {
	open: boolean;
	onOpenChange: (open: boolean) => void;
	projectId: string;
	hostId?: string;
	config: ProjectConfig;
	roster: CoordinatorRoster;
	onStarted: () => void;
}) {
	const { t } = useTranslation();
	const [form, setForm] = useState<SpawnCoordinatorForm>(initialForm);
	const [submitting, setSubmitting] = useState(false);
	const [submitError, setSubmitError] = useState<string | null>(null);
	const [started, setStarted] = useState<{ id: string } | null>(null);
	const allowedHosts = config.coordinators?.allowedHosts ?? [];
	const limits = { workersPerCoordinator: roster.capacity.maxWorkersPerCoordinator, allowedHosts };
	const errors: SpawnCoordinatorErrors = validateSpawnCoordinator(form, limits);
	const hasErrors = Object.keys(errors).length > 0;
	const profiles = coordinatorsFormFrom(config.coordinators).agentProfiles;
	const hosts = useRemoteHosts().hosts.filter((host) => allowedHosts.length === 0 || allowedHosts.includes(host.hostId));
	const update = (patch: Partial<SpawnCoordinatorForm>) => setForm((current) => ({ ...current, ...patch }));
	const setSlot = (index: number, patch: Partial<SpawnCoordinatorForm["hostSlots"][number]>) =>
		update({ hostSlots: form.hostSlots.map((row, position) => (position === index ? { ...row, ...patch } : row)) });

	const submit = async () => {
		setSubmitting(true);
		setSubmitError(null);
		try {
			const session = await spawnCoordinatorOnBehalfOfRoot(projectId, form, hostId);
			setStarted({ id: session.id });
			onStarted();
		} catch (failure) {
			const key = coordinatorErrorKey(failure);
			setSubmitError(key ? t(key) : apiErrorMessage(failure, t("coordinators.errors.startFailed")));
		} finally {
			setSubmitting(false);
		}
	};

	const effectiveLimit = form.workerLimit.trim() === "" ? roster.capacity.maxWorkersPerCoordinator : Number(form.workerLimit);
	return (
		<Dialog open={open} onOpenChange={(next) => { if (!submitting) onOpenChange(next); }}>
			<DialogPortal>
				<DialogOverlay />
				<DialogContent
					className={cn(settingsDialogContentClass, "w-[min(560px,calc(100vw-24px))] max-h-[calc(100vh-48px)] overflow-y-auto")}
					showCloseButton={false}
				>
					<DialogClose asChild>
						<button aria-label={t("confirm.close")} className="settings-dialog-close-button settings-close-button" disabled={submitting} type="button">
							<X className="size-4" aria-hidden="true" />
						</button>
					</DialogClose>
					<div className={cn(settingsDialogHeaderClass, "p-5 pr-12")}>
						<DialogTitle className="settings-dialog-title text-base">{t("coordinators.start.title")}</DialogTitle>
						<DialogDescription className="text-control leading-5 text-settings-muted">
							{t("coordinators.start.description")}
						</DialogDescription>
					</div>

					{started ? (
						<div className={cn(settingsDialogBodyClass, "flex flex-col gap-2 px-5 pb-4")} role="status">
							<p className="text-sm">{t("coordinators.start.started", { id: started.id })}</p>
							<p className="text-xs text-settings-muted">
								{t("coordinators.start.audit", {
									origin: t("coordinators.start.originUi"),
									root: roster.rootSessionId ?? "",
									scope: form.scope.trim(),
									limit: effectiveLimit,
								})}
							</p>
						</div>
					) : (
						<div className={cn(settingsDialogBodyClass, "flex flex-col gap-4 px-5 pt-0 pb-3")}>
							<div className="flex flex-col gap-1.5">
								<Label htmlFor="start-scope">{t("coordinators.start.scope")}</Label>
								<Input
									aria-invalid={errors.scope ? true : undefined}
									id="start-scope"
									maxLength={COORDINATOR_SCOPE_MAX + 1}
									onChange={(event) => update({ scope: event.target.value })}
									value={form.scope}
								/>
								{errors.scope ? <p className="text-sm text-error">{t(errors.scope)}</p> : null}
							</div>

							<div className="flex flex-col gap-1.5">
								<Label htmlFor="start-limit">{t("coordinators.start.workerLimit")}</Label>
								<Input
									className="w-24"
									id="start-limit"
									inputMode="numeric"
									onChange={(event) => update({ workerLimit: event.target.value })}
									placeholder={String(roster.capacity.maxWorkersPerCoordinator)}
									value={form.workerLimit}
								/>
								<p className="text-xs text-settings-muted">
									{t("coordinators.start.workerLimitHint", { max: roster.capacity.maxWorkersPerCoordinator, hard: COORDINATOR_HARD_LIMITS.workersPerCoordinator })}
								</p>
								{errors.workerLimit ? <p className="text-sm text-error">{t(errors.workerLimit)}</p> : null}
							</div>

							<div className="flex flex-col gap-2">
								<span className="text-sm font-medium">{t("coordinators.start.hostSlots")}</span>
								{form.hostSlots.map((row, index) => (
									<div className="flex items-center gap-2" key={index}>
										<Select onValueChange={(hostId) => setSlot(index, { hostId })} value={row.hostId}>
											<SelectTrigger aria-label={t("coordinators.start.host")} className="min-w-0 flex-1">
												<SelectValue placeholder={t("coordinators.start.host")} />
											</SelectTrigger>
											<SelectContent>
												{hosts.map((host) => (
													<SelectItem key={host.hostId} value={host.hostId}>
														{host.label}
													</SelectItem>
												))}
											</SelectContent>
										</Select>
										<Input
											aria-label={t("coordinators.start.slots")}
											className="w-20"
											inputMode="numeric"
											onChange={(event) => setSlot(index, { slots: event.target.value })}
											value={row.slots}
										/>
										<Button
											onClick={() => update({ hostSlots: form.hostSlots.filter((_, position) => position !== index) })}
											size="sm"
											type="button"
											variant="ghost"
										>
											{t("coordinators.start.removeSlot")}
										</Button>
									</div>
								))}
								{errors.hostSlots ? <p className="text-sm text-error">{t(errors.hostSlots)}</p> : null}
								<div>
									<Button
										onClick={() => update({ hostSlots: [...form.hostSlots, { hostId: "", slots: "1" }] })}
										size="sm"
										type="button"
										variant="outline"
									>
										{t("coordinators.start.addSlot")}
									</Button>
								</div>
							</div>

							<div className="flex flex-col gap-1.5">
								<Label htmlFor="start-agent">{t("coordinators.start.coordinatorAgent")}</Label>
								<Select onValueChange={(mode) => update({ coordinatorAgent: mode as CoordinatorAgentMode })} value={form.coordinatorAgent}>
									<SelectTrigger className="w-full min-w-0" id="start-agent">
										<SelectValue />
									</SelectTrigger>
									<SelectContent>
										<SelectItem value="inherit">{t("coordinators.start.agentInherit")}</SelectItem>
										<SelectItem value="profile">{t("coordinators.start.agentProfile")}</SelectItem>
										<SelectItem value="custom">{t("coordinators.start.agentCustom")}</SelectItem>
									</SelectContent>
								</Select>
								{form.coordinatorAgent === "profile" ? (
									<Select onValueChange={(coordinatorProfileId) => update({ coordinatorProfileId })} value={form.coordinatorProfileId}>
										<SelectTrigger aria-label={t("coordinators.profilePick")} className="w-full min-w-0">
											<SelectValue placeholder={t("coordinators.profilePick")} />
										</SelectTrigger>
										<SelectContent>
											{profiles.filter((profile) => profile.id.trim() !== "").map((profile) => (
												<SelectItem key={profile.id} value={profile.id}>
													{profile.label.trim() || profile.id}
												</SelectItem>
											))}
										</SelectContent>
									</Select>
								) : null}
								{form.coordinatorAgent === "custom" ? (
									<div className="grid grid-cols-3 gap-2">
										<Input aria-label={t("coordinators.start.harness")} onChange={(event) => update({ customHarness: event.target.value })} placeholder={t("coordinators.start.harness")} value={form.customHarness} />
										<Input aria-label={t("coordinators.start.model")} onChange={(event) => update({ customModel: event.target.value })} placeholder={t("coordinators.start.model")} value={form.customModel} />
										<Input aria-label={t("coordinators.start.effort")} onChange={(event) => update({ customEffort: event.target.value })} placeholder={t("coordinators.start.effort")} value={form.customEffort} />
									</div>
								) : null}
								{errors.coordinatorAgent ? <p className="text-sm text-error">{t(errors.coordinatorAgent)}</p> : null}
							</div>

							<AgentSelectionField
								id="start-worker-agent"
								label={t("coordinators.start.workerAgent")}
								onChange={(workerAgent) => update({ workerAgent })}
								profiles={profiles}
								value={form.workerAgent}
							/>

							<div className="flex flex-col gap-1.5">
								<Label htmlFor="start-brief">{t("coordinators.start.brief")}</Label>
								<textarea
									aria-invalid={errors.brief ? true : undefined}
									className="min-h-48 w-full rounded-md border border-input bg-transparent p-2 font-mono text-xs"
									id="start-brief"
									onChange={(event) => update({ brief: event.target.value })}
									value={form.brief}
								/>
								{errors.brief ? <p className="text-sm text-error">{t(errors.brief)}</p> : null}
							</div>

							{submitError ? <p className="text-sm text-error" role="alert">{submitError}</p> : null}
						</div>
					)}

					<div className={cn(settingsDialogFooterClass, "gap-2 border-t-0 p-4 pt-1")}>
						<DialogClose asChild>
							<Button disabled={submitting} type="button" variant="ghost">
								{started ? t("coordinators.start.done") : t("confirm.cancel")}
							</Button>
						</DialogClose>
						{started ? null : (
							<Button disabled={hasErrors || submitting} onClick={() => void submit()} type="button">
								{t("coordinators.start.submit")}
							</Button>
						)}
					</div>
				</DialogContent>
			</DialogPortal>
		</Dialog>
	);
}
