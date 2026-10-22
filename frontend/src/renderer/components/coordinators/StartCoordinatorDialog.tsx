import { useState } from "react";
import { useTranslation } from "react-i18next";
import { useRemoteHosts } from "../../hooks/useRemoteHosts";
import { apiErrorMessage } from "../../lib/api-client";
import { clientForSessionHost } from "../../lib/host-clients";
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
import {
	ACCEPTANCE_MARKER,
	type BriefTask,
	COORDINATOR_BRIEF_TEMPLATE,
	acceptanceCriteriaFrom,
	defaultBranchPrefix,
	renderBrief,
} from "./brief-template";

type CoordinatorAgentMode = SpawnCoordinatorForm["coordinatorAgent"];

const emptySelection = (): SelectionForm => ({ mode: "inherit", profileId: "", profileIds: [] });

const emptyTask = (): BriefTask => ({ taskId: "", title: "", acceptance: "", files: "" });

/** Comma or space separated issue numbers; null when one is not valid. */
const parseIssueNumbers = (input: string): number[] | null => {
	const parts = input.split(/[\s,]+/).filter((part) => part !== "");
	if (parts.length === 0 || !parts.every((part) => /^\d+$/.test(part) && Number(part) > 0)) return null;
	return parts.map(Number);
};

const isBlankTask = (task: BriefTask) => task.taskId === "" && task.title === "" && task.acceptance === "" && task.files === "";

/** Regenerates the brief from the fields unless edited by hand. */
const withRenderedBrief = (form: SpawnCoordinatorForm): SpawnCoordinatorForm => {
	if (form.briefEdited) return form;
	const { brief, unfilled } = renderBrief(form);
	return { ...form, brief, briefPlaceholders: unfilled };
};

const initialForm = (integrationBranch: string): SpawnCoordinatorForm =>
	withRenderedBrief({
		name: "",
		scope: "",
		outOfScope: "",
		integrationBranch,
		branchPrefix: "",
		tasks: [emptyTask()],
		briefEdited: false,
		briefPlaceholders: [],
		workerLimit: "",
		hostSlots: [{ hostId: "", slots: "1" }],
		coordinatorAgent: "inherit",
		coordinatorProfileId: "",
		customHarness: "",
		customModel: "",
		customEffort: "",
		workerAgent: emptySelection(),
		brief: "",
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
	const [form, setForm] = useState<SpawnCoordinatorForm>(() => initialForm(config.defaultBranch ?? ""));
	const [submitting, setSubmitting] = useState(false);
	const [submitError, setSubmitError] = useState<string | null>(null);
	const [started, setStarted] = useState<{ id: string } | null>(null);
	const [issueNumbers, setIssueNumbers] = useState("");
	const [issueLoading, setIssueLoading] = useState(false);
	const [issueMessage, setIssueMessage] = useState<string | null>(null);
	const allowedHosts = config.coordinators?.allowedHosts ?? [];
	const profiles = coordinatorsFormFrom(config.coordinators).agentProfiles;
	const limits = {
		workersPerCoordinator: roster.capacity.maxWorkersPerCoordinator,
		allowedHosts,
		profileIds: profiles.map((profile) => profile.id),
	};
	const errors: SpawnCoordinatorErrors = validateSpawnCoordinator(form, limits);
	const hasErrors = Object.keys(errors).length > 0;
	const hosts = useRemoteHosts().hosts.filter((host) => allowedHosts.length === 0 || allowedHosts.includes(host.hostId));
	const update = (patch: Partial<SpawnCoordinatorForm>) =>
		setForm((current) => withRenderedBrief({ ...current, ...patch }));
	const setTask = (index: number, patch: Partial<BriefTask>) =>
		update({ tasks: form.tasks.map((task, position) => (position === index ? { ...task, ...patch } : task)) });
	const setSlot = (index: number, patch: Partial<SpawnCoordinatorForm["hostSlots"][number]>) =>
		update({ hostSlots: form.hostSlots.map((row, position) => (position === index ? { ...row, ...patch } : row)) });

	// One task row per issue: title and acceptance criteria. A missing
	// section leaves the marker, which blocks Start.
	const loadIssues = async () => {
		const numbers = parseIssueNumbers(issueNumbers);
		if (numbers === null) {
			setIssueMessage(t("coordinators.start.issueNumbersInvalid"));
			return;
		}
		setIssueLoading(true);
		setIssueMessage(null);
		try {
			const loaded: BriefTask[] = [];
			const missing: number[] = [];
			for (const number of numbers) {
				const { data, error } = await clientForSessionHost(hostId).GET("/api/v1/projects/{id}/issues/{number}", {
					params: { path: { id: projectId, number } },
				});
				if (error || !data) {
					setIssueMessage(apiErrorMessage(error, t("coordinators.start.issueLoadFailed", { number })));
					return;
				}
				const acceptance = acceptanceCriteriaFrom(data.body);
				if (acceptance === null) missing.push(number);
				loaded.push({ taskId: `#${number}`, title: data.title, acceptance: acceptance ?? "", files: "" });
			}
			update({ tasks: [...form.tasks.filter((task) => !isBlankTask(task)), ...loaded] });
			if (missing.length > 0) {
				setIssueMessage(t("coordinators.start.issueNoAcceptance", { numbers: missing.map((number) => `#${number}`).join(", ") }));
			}
		} finally {
			setIssueLoading(false);
		}
	};

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
								<Label htmlFor="start-name">{t("coordinators.start.name")}</Label>
								<Input id="start-name" onChange={(event) => update({ name: event.target.value })} value={form.name} />
							</div>

							<div className="flex flex-col gap-1.5">
								<Label htmlFor="start-out-of-scope">{t("coordinators.start.outOfScope")}</Label>
								<Input id="start-out-of-scope" onChange={(event) => update({ outOfScope: event.target.value })} value={form.outOfScope} />
							</div>

							<div className="flex flex-col gap-1.5">
								<Label htmlFor="start-integration">{t("coordinators.start.integrationBranch")}</Label>
								<Input id="start-integration" onChange={(event) => update({ integrationBranch: event.target.value })} value={form.integrationBranch} />
							</div>

							<div className="flex flex-col gap-1.5">
								<Label htmlFor="start-prefix">{t("coordinators.start.branchPrefix")}</Label>
								<Input
									id="start-prefix"
									onChange={(event) => update({ branchPrefix: event.target.value })}
									placeholder={defaultBranchPrefix(form.scope)}
									value={form.branchPrefix}
								/>
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
									<>
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
									{errors.coordinatorAgent ? (
										<p className="text-sm text-error" role="alert">{t(errors.coordinatorAgent)}</p>
									) : null}
									</>
								) : null}
								{form.coordinatorAgent === "custom" ? (
									<div className="grid grid-cols-3 gap-2">
										<Input aria-label={t("coordinators.start.harness")} onChange={(event) => update({ customHarness: event.target.value })} placeholder={t("coordinators.start.harness")} value={form.customHarness} />
										<Input aria-label={t("coordinators.start.model")} onChange={(event) => update({ customModel: event.target.value })} placeholder={t("coordinators.start.model")} value={form.customModel} />
										<Input aria-label={t("coordinators.start.effort")} onChange={(event) => update({ customEffort: event.target.value })} placeholder={t("coordinators.start.effort")} value={form.customEffort} />
									</div>
								) : null}
								{form.coordinatorAgent === "custom" && errors.coordinatorAgent ? (
									<p className="text-sm text-error" role="alert">{t(errors.coordinatorAgent)}</p>
								) : null}
							</div>

							<AgentSelectionField
								error={errors.workerAgent}
								id="start-worker-agent"
								label={t("coordinators.start.workerAgent")}
								onChange={(workerAgent) => update({ workerAgent })}
								profiles={profiles}
								value={form.workerAgent}
							/>

							<div className="flex flex-col gap-2">
								<span className="text-sm font-medium">{t("coordinators.start.tasks")}</span>
								<div className="flex items-end gap-2">
									<div className="flex flex-1 flex-col gap-1">
										<Label htmlFor="start-issues">{t("coordinators.start.issueNumbers")}</Label>
										<Input id="start-issues" onChange={(event) => setIssueNumbers(event.target.value)} value={issueNumbers} />
									</div>
									<Button disabled={issueLoading || submitting} onClick={() => void loadIssues()} size="sm" type="button" variant="outline">
										{t("coordinators.start.loadFromIssue")}
									</Button>
								</div>
								{issueMessage ? <p className="text-sm text-error" role="alert">{issueMessage}</p> : null}
								{form.tasks.map((task, index) => (
									<div className="grid grid-cols-2 gap-2 rounded-md border border-input p-2" key={index}>
										<Input aria-label={t("coordinators.start.taskId")} onChange={(event) => setTask(index, { taskId: event.target.value })} placeholder={t("coordinators.start.taskId")} value={task.taskId} />
										<Input aria-label={t("coordinators.start.taskTitle")} onChange={(event) => setTask(index, { title: event.target.value })} placeholder={t("coordinators.start.taskTitle")} value={task.title} />
										<Input aria-label={t("coordinators.start.taskAcceptance")} className="col-span-2" onChange={(event) => setTask(index, { acceptance: event.target.value })} placeholder={ACCEPTANCE_MARKER} value={task.acceptance} />
										<Input aria-label={t("coordinators.start.taskFiles")} className="col-span-2" onChange={(event) => setTask(index, { files: event.target.value })} placeholder={t("coordinators.start.taskFiles")} value={task.files} />
										{form.tasks.length > 1 ? (
											<Button
												className="col-span-2 justify-self-end"
												onClick={() => update({ tasks: form.tasks.filter((_, position) => position !== index) })}
												size="sm"
												type="button"
												variant="ghost"
											>
												{t("coordinators.start.removeTask")}
											</Button>
										) : null}
									</div>
								))}
								<div>
									<Button onClick={() => update({ tasks: [...form.tasks, emptyTask()] })} size="sm" type="button" variant="outline">
										{t("coordinators.start.addTask")}
									</Button>
								</div>
							</div>

							<div className="flex flex-col gap-1.5">
								<div className="flex items-center justify-between gap-2">
									<Label htmlFor="start-brief">{t("coordinators.start.brief")}</Label>
									<Button onClick={() => update({ brief: COORDINATOR_BRIEF_TEMPLATE, briefEdited: true })} size="sm" type="button" variant="ghost">
										{t("coordinators.start.resetBrief")}
									</Button>
								</div>
								{form.briefEdited ? <p className="text-xs text-settings-muted">{t("coordinators.start.briefEdited")}</p> : null}
								<textarea
									aria-invalid={errors.brief ? true : undefined}
									className="min-h-48 w-full rounded-md border border-input bg-transparent p-2 font-mono text-xs"
									id="start-brief"
									onChange={(event) => update({ brief: event.target.value, briefEdited: true })}
									value={form.brief}
								/>
								{errors.brief ? <p className="text-sm text-error">{t(errors.brief, { placeholder: errors.placeholder })}</p> : null}
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
