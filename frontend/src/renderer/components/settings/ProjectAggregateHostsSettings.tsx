import { ProjectSettingsRow, ProjectSettingsSection } from "@aoagents/product-ui";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Badge } from "../ui/badge";
import { Button } from "../ui/button";
import { Input } from "../ui/input";
import { ProjectToggleRow } from "./ProjectAgentRoleControls";

export type AggregateRef = { hostId: string; projectId: string };

/** A project discovered on a paired host that is not hidden. */
export type AggregateDiscoveredRow = AggregateRef & {
	hostLabel: string;
	matchedBy?: string;
	stale: boolean;
};

type AggregateHostsSettingsProps = {
	discovery: boolean;
	discovered: AggregateDiscoveredRow[];
	hidden: AggregateRef[];
	manual: AggregateRef[];
	onDiscoveryChange: (discovery: boolean) => void;
	onHide: (ref: AggregateRef) => void;
	onShow: (ref: AggregateRef) => void;
	onManualAdd: (ref: AggregateRef) => void;
	onManualRemove: (index: number) => void;
};

// Discovery, hidden rows, and manual rows for the project's aggregate board.
// Hide and show write aggregateExcludes through the normal settings autosave.
export function ProjectAggregateHostsSettings({ discovery, discovered, hidden, manual, onDiscoveryChange, onHide, onShow, onManualAdd, onManualRemove }: AggregateHostsSettingsProps) {
	const { t } = useTranslation();
	return <ProjectSettingsSection title={t("settings.project.aggregateHosts", { defaultValue: "Aggregate project hosts" })} grouped>
		<ProjectToggleRow id="project-aggregate-discovery" label={t("settings.project.aggregateDiscoveryToggle", { defaultValue: "Discover same project on paired hosts" })} description={t("settings.project.aggregateDiscoveryHelp", { defaultValue: "Matches a project on a paired host by the same repository. Without a repository on either side, it matches by project ID." })} checked={discovery} onCheckedChange={onDiscoveryChange} />
		{discovery && discovered.map((row) => <ProjectSettingsRow key={`discovered:${row.hostId}:${row.projectId}`} label={`${row.hostLabel} · ${row.projectId}`}>
			<div className="flex min-w-0 items-center gap-2">
				<Badge variant="outline">{t("settings.project.aggregateDiscovered", { defaultValue: "Discovered" })}</Badge>
				{row.matchedBy === "projectId" ? <Badge variant="outline">{t("settings.project.aggregateMatchedProjectId", { defaultValue: "Matched by project ID" })}</Badge> : null}
				{row.stale ? <Badge variant="warning">{t("settings.project.aggregateStale", { defaultValue: "Stale" })}</Badge> : null}
				<Button type="button" variant="ghost" size="sm" aria-label={t("settings.project.aggregateHide", { host: row.hostLabel, project: row.projectId, defaultValue: `Hide ${row.hostLabel} / ${row.projectId}` })} onClick={() => onHide({ hostId: row.hostId, projectId: row.projectId })}>{t("settings.project.aggregateHideAction", { defaultValue: "Hide" })}</Button>
			</div>
		</ProjectSettingsRow>)}
		{hidden.map((ref) => <ProjectSettingsRow key={`hidden:${ref.hostId}:${ref.projectId}`} label={`${ref.hostId} · ${ref.projectId}`}>
			<div className="flex min-w-0 items-center gap-2">
				<Badge variant="outline">{t("settings.project.aggregateHidden", { defaultValue: "Hidden" })}</Badge>
				<Button type="button" variant="ghost" size="sm" aria-label={t("settings.project.aggregateShow", { host: ref.hostId, project: ref.projectId, defaultValue: `Show ${ref.hostId} / ${ref.projectId}` })} onClick={() => onShow(ref)}>{t("settings.project.aggregateShowAction", { defaultValue: "Show" })}</Button>
			</div>
		</ProjectSettingsRow>)}
		{manual.map((entry, index) => <ProjectSettingsRow key={`manual:${entry.hostId}:${entry.projectId}`} label={`${entry.hostId} · ${entry.projectId}`}>
			<Button type="button" variant="ghost" size="sm" className="text-destructive hover:bg-destructive/10 hover:text-destructive" aria-label={t("settings.project.aggregateRemove", { hostId: entry.hostId, projectId: entry.projectId, defaultValue: `Remove ${entry.hostId} / ${entry.projectId}` })} onClick={() => onManualRemove(index)}>{t("shell.remove")}</Button>
		</ProjectSettingsRow>)}
		<AggregateProjectEditor existing={manual} onAdd={onManualAdd} />
	</ProjectSettingsSection>;
}

function AggregateProjectEditor({ existing, onAdd }: {
	existing: AggregateRef[];
	onAdd: (ref: AggregateRef) => void;
}) {
	const { t } = useTranslation();
	const [hostId, setHostId] = useState("");
	const [projectId, setProjectId] = useState("");
	const duplicate = existing.some((entry) => entry.hostId === hostId.trim() && entry.projectId === projectId.trim());
	return <div className="grid gap-2 px-3 py-2 sm:grid-cols-[1fr_1fr_auto]">
		<Input aria-label={t("settings.project.aggregateHostId", { defaultValue: "Host ID" })} value={hostId} onChange={(event) => setHostId(event.currentTarget.value)} placeholder={t("settings.project.aggregateHostId", { defaultValue: "Host ID" })} />
		<Input aria-label={t("settings.project.aggregateProjectId", { defaultValue: "Project ID" })} value={projectId} onChange={(event) => setProjectId(event.currentTarget.value)} placeholder={t("settings.project.aggregateProjectId", { defaultValue: "Project ID" })} />
		<Button type="button" variant="outline" disabled={!hostId.trim() || !projectId.trim() || duplicate} onClick={() => { onAdd({ hostId: hostId.trim(), projectId: projectId.trim() }); setHostId(""); setProjectId(""); }}>{t("settings.project.aggregateAdd", { defaultValue: "Add project" })}</Button>
	</div>;
}
