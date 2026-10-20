import { type FormEvent, useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { useRemoteHosts } from "../../hooks/useRemoteHosts";
import { apiClient, apiErrorMessage } from "../../lib/api-client";
import { clientForHost } from "../../lib/host-clients";
import {
	COORDINATOR_HARD_LIMITS,
	coordinatorsConfigFrom,
	coordinatorsFormFrom,
	type CoordinatorsForm,
	type CoordinatorsFormErrorKey,
	type ProjectConfig,
	validateCoordinatorsForm,
	withCoordinatorsConfig,
} from "../../lib/coordinators";
import type { ProjectSettingsSaveState } from "../ProjectSettingsForm";
import { AgentProfilesEditor } from "./AgentProfilesEditor";
import { AgentSelectionField } from "./AgentSelectionField";
import { CoordinatorRoster } from "./CoordinatorRoster";
import { Button } from "../ui/button";
import { Checkbox } from "../ui/checkbox";
import { Input } from "../ui/input";
import { Label } from "../ui/label";
import { Switch } from "../ui/switch";

/** Coordinators project settings: limits, hosts, agents and roster. */
export function CoordinatorsSettingsForm({
	projectId,
	projectName,
	hostId,
	config,
	onSaved,
	onSaveState,
}: {
	projectId: string;
	projectName: string;
	hostId?: string;
	config: ProjectConfig;
	onSaved: () => void;
	onSaveState?: (state: ProjectSettingsSaveState) => void;
}) {
	const { t } = useTranslation();
	const hosts = useRemoteHosts().hosts;
	// The last saved form. A successful save moves it forward, so only edits
	// made after that count as unsaved.
	const [baseline, setBaseline] = useState(() => coordinatorsFormFrom(config.coordinators));
	const [form, setForm] = useState<CoordinatorsForm>(baseline);
	const [saving, setSaving] = useState(false);
	const [saveError, setSaveError] = useState<string | null>(null);
	const [saved, setSaved] = useState(false);
	const errors = validateCoordinatorsForm(form);
	const hasErrors = Object.keys(errors).length > 0;
	const dirty = JSON.stringify(form) !== JSON.stringify(baseline);
	const blocked = dirty && hasErrors;
	// The dialog submits this form before it switches section or closes. A
	// draft that cannot be saved reports "failed", so the switch or close is
	// held and the reason stays on screen instead of dropping the edits.
	useEffect(() => {
		onSaveState?.({
			phase: saving ? "saving" : saveError || blocked ? "failed" : saved && !dirty ? "saved" : "idle",
			dirty,
			error: saveError ?? (blocked ? t("coordinators.errors.fixBeforeSave") : undefined),
		});
	}, [dirty, blocked, saveError, saved, saving, onSaveState, t]);
	const update = (patch: Partial<CoordinatorsForm>) => {
		setSaved(false);
		setForm((current) => ({ ...current, ...patch }));
	};

	const save = async () => {
		const snapshot = form;
		setSaving(true);
		setSaveError(null);
		setSaved(false);
		try {
			const next = withCoordinatorsConfig(config, coordinatorsConfigFrom(snapshot));
			const { error } = await (hostId ? clientForHost(hostId) : apiClient).PUT("/api/v1/projects/{id}", {
				params: { path: { id: projectId } },
				body: { displayName: projectName, config: next },
			});
			if (error) throw new Error(apiErrorMessage(error));
			setBaseline(snapshot);
			setSaved(true);
			onSaved();
		} catch (error) {
			setSaveError(error instanceof Error ? error.message : t("coordinators.saveFailed"));
		} finally {
			setSaving(false);
		}
	};

	const allowedHostIds = new Set(form.allowedHosts);
	const toggleHost = (id: string, on: boolean) =>
		update({ allowedHosts: on ? [...form.allowedHosts, id] : form.allowedHosts.filter((candidate) => candidate !== id) });

	const onSubmit = (event: FormEvent<HTMLFormElement>) => {
		event.preventDefault();
		if (hasErrors) {
			setSaved(false);
			return;
		}
		if (dirty) void save();
	};

	return (
		<div className="flex flex-col gap-6" data-testid="coordinators-settings">
		<form className="flex flex-col gap-6" id="project-settings-form" onSubmit={onSubmit}>
			<div className="flex items-center justify-between gap-4">
				<Label htmlFor="coordinators-enabled">{t("coordinators.enable")}</Label>
				<Switch checked={form.enabled} id="coordinators-enabled" onCheckedChange={(enabled) => update({ enabled })} />
			</div>

			<section className="flex flex-col gap-3">
				<h3 className="text-sm font-medium">{t("coordinators.limits")}</h3>
				<LimitField
					error={errors.maxCoordinators}
					hard={COORDINATOR_HARD_LIMITS.coordinators}
					id="coordinators-max"
					label={t("coordinators.maxCoordinators")}
					onChange={(maxCoordinators) => update({ maxCoordinators })}
					value={form.maxCoordinators}
				/>
				<LimitField
					error={errors.maxWorkersPerCoordinator}
					hard={COORDINATOR_HARD_LIMITS.workersPerCoordinator}
					id="coordinators-per"
					label={t("coordinators.maxWorkersPerCoordinator")}
					onChange={(maxWorkersPerCoordinator) => update({ maxWorkersPerCoordinator })}
					value={form.maxWorkersPerCoordinator}
				/>
				<LimitField
					error={errors.maxWorkers}
					hard={COORDINATOR_HARD_LIMITS.workers}
					id="coordinators-workers"
					label={t("coordinators.maxWorkers")}
					onChange={(maxWorkers) => update({ maxWorkers })}
					value={form.maxWorkers}
				/>
			</section>

			<section className="flex flex-col gap-2">
				<h3 className="text-sm font-medium">{t("coordinators.allowedHosts")}</h3>
				<p className="text-sm text-settings-muted">{t("coordinators.allowedHostsHint")}</p>
				{hosts.length === 0 ? (
					<p className="text-sm text-settings-muted">{t("coordinators.noHosts")}</p>
				) : (
					hosts.map((host) => (
						<label className="flex items-center gap-2 text-sm" key={host.hostId}>
							<Checkbox
								checked={allowedHostIds.has(host.hostId)}
								onCheckedChange={(on) => toggleHost(host.hostId, on === true)}
							/>
							{host.label}
						</label>
					))
				)}
			</section>

			<section className="flex flex-col gap-2">
				<div className="flex items-center justify-between gap-4">
					<Label htmlFor="coordinators-local">{t("coordinators.allowLocalWorkers")}</Label>
					<Switch
						checked={form.allowLocalWorkers}
						id="coordinators-local"
						onCheckedChange={(allowLocalWorkers) => update({ allowLocalWorkers })}
					/>
				</div>
				{form.allowLocalWorkers ? (
					<p className="text-sm text-warning" role="note">{t("coordinators.allowLocalWorkersWarning")}</p>
				) : null}
			</section>

			<section className="flex flex-col gap-3">
				<h3 className="text-sm font-medium">{t("coordinators.defaults")}</h3>
				<AgentSelectionField
					id="coordinators-agent"
					label={t("coordinators.coordinatorAgent")}
					onChange={(coordinatorAgent) => update({ coordinatorAgent })}
					profiles={form.agentProfiles}
					value={form.coordinatorAgent}
					error={errors.selections?.coordinatorAgent}
				/>
				<AgentSelectionField
					id="coordinators-worker"
					label={t("coordinators.workerAgent")}
					onChange={(workerAgent) => update({ workerAgent })}
					profiles={form.agentProfiles}
					value={form.workerAgent}
					error={errors.selections?.workerAgent}
				/>
				<AgentSelectionField
					id="coordinators-root-worker"
					label={t("coordinators.rootWorkerAgent")}
					onChange={(rootWorkerAgent) => update({ rootWorkerAgent })}
					profiles={form.agentProfiles}
					value={form.rootWorkerAgent}
					error={errors.selections?.rootWorkerAgent}
				/>
			</section>

			<section className="flex flex-col gap-3">
				<h3 className="text-sm font-medium">{t("coordinators.profiles")}</h3>
				<AgentProfilesEditor
					error={errors.profiles}
					onChange={(agentProfiles) => update({ agentProfiles })}
					profiles={form.agentProfiles}
				/>
			</section>

			<div className="flex items-center gap-3">
				<Button disabled={!dirty || hasErrors || saving} type="submit">
					{t("coordinators.save")}
				</Button>
				<span className="text-sm" role="status">
					{saveError ? <span className="text-error">{saveError}</span> : saved ? t("coordinators.saved") : null}
				</span>
			</div>

		</form>

			<CoordinatorRoster config={config} hostId={hostId} projectId={projectId} />
		</div>
	);
}

function LimitField({
	id,
	label,
	value,
	hard,
	error,
	onChange,
}: {
	id: string;
	label: string;
	value: string;
	hard: number;
	error?: CoordinatorsFormErrorKey;
	onChange: (value: string) => void;
}) {
	const { t } = useTranslation();
	return (
		<div className="flex flex-col gap-1.5">
			<Label htmlFor={id}>{label}</Label>
			<div className="flex items-center gap-3">
				<Input
					aria-describedby={`${id}-hint`}
					aria-invalid={error ? true : undefined}
					className="w-24"
					id={id}
					inputMode="numeric"
					onChange={(event) => onChange(event.target.value)}
					value={value}
				/>
				<span className="text-sm text-settings-muted" id={`${id}-hint`}>
					{t("coordinators.hardLimit", { max: hard })}
				</span>
			</div>
			{error ? <p className="text-sm text-error">{t(error)}</p> : null}
		</div>
	);
}
