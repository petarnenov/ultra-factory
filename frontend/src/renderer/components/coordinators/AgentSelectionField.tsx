import { useTranslation } from "react-i18next";
import type { ProfileForm, SelectionForm, SelectionMode } from "../../lib/coordinators";
import { Checkbox } from "../ui/checkbox";
import { Label } from "../ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "../ui/select";

const MODE_LABEL_KEYS = {
	inherit: "coordinators.mode.inherit",
	fixed: "coordinators.mode.fixed",
	random: "coordinators.mode.random",
} as const;

export const profileLabel = (profile: Pick<ProfileForm, "id" | "label">) => profile.label.trim() || profile.id;

/** Inherit, a fixed profile, or random over a subset of the project profiles. */
export function AgentSelectionField({
	id,
	label,
	value,
	profiles,
	modes = ["inherit", "fixed", "random"],
	disabled,
	onChange,
}: {
	id: string;
	label: string;
	value: SelectionForm;
	profiles: ProfileForm[];
	modes?: SelectionMode[];
	disabled?: boolean;
	onChange: (next: SelectionForm) => void;
}) {
	const { t } = useTranslation();
	const usable = profiles.filter((profile) => profile.id.trim() !== "");
	return (
		<div className="flex flex-col gap-1.5">
			<Label htmlFor={`${id}-mode`}>{label}</Label>
			<Select
				disabled={disabled}
				onValueChange={(mode) => onChange({ ...value, mode: mode as SelectionMode })}
				value={value.mode}
			>
				<SelectTrigger className="w-full min-w-0" id={`${id}-mode`}>
					<SelectValue />
				</SelectTrigger>
				<SelectContent>
					{modes.map((mode) => (
						<SelectItem key={mode} value={mode}>
							{t(MODE_LABEL_KEYS[mode])}
						</SelectItem>
					))}
				</SelectContent>
			</Select>
			{value.mode === "fixed" ? (
				<Select
					disabled={disabled}
					onValueChange={(profileId) => onChange({ ...value, profileId })}
					value={value.profileId}
				>
					<SelectTrigger aria-label={t("coordinators.profilePick")} className="w-full min-w-0">
						<SelectValue placeholder={t("coordinators.profilePick")} />
					</SelectTrigger>
					<SelectContent>
						{usable.map((profile) => (
							<SelectItem key={profile.id} value={profile.id}>
								{profileLabel(profile)}
							</SelectItem>
						))}
					</SelectContent>
				</Select>
			) : null}
			{value.mode === "random" ? (
				<fieldset className="flex flex-col gap-1.5">
					<legend className="text-sm text-settings-muted">{t("coordinators.randomPick")}</legend>
					{usable.map((profile) => {
						const checked = value.profileIds.includes(profile.id);
						return (
							<label className="flex items-center gap-2 text-sm" key={profile.id}>
								<Checkbox
									checked={checked}
									disabled={disabled}
									onCheckedChange={(next) =>
										onChange({
											...value,
											profileIds: next
												? [...value.profileIds, profile.id]
												: value.profileIds.filter((candidate) => candidate !== profile.id),
										})
									}
								/>
								{profileLabel(profile)}
							</label>
						);
					})}
				</fieldset>
			) : null}
		</div>
	);
}
