import { useTranslation } from "react-i18next";
import type { CoordinatorsFormErrorKey, ProfileForm } from "../../lib/coordinators";
import { Button } from "../ui/button";
import { Input } from "../ui/input";
import { Label } from "../ui/label";

const emptyProfile = (): ProfileForm => ({ id: "", label: "", harness: "", model: "", effort: "", weight: "" });

/** Shared profile list for coordinators and their workers. */
export function AgentProfilesEditor({
	profiles,
	error,
	disabled,
	onChange,
}: {
	profiles: ProfileForm[];
	error?: CoordinatorsFormErrorKey;
	disabled?: boolean;
	onChange: (next: ProfileForm[]) => void;
}) {
	const { t } = useTranslation();
	const update = (index: number, patch: Partial<ProfileForm>) =>
		onChange(profiles.map((profile, position) => (position === index ? { ...profile, ...patch } : profile)));
	return (
		<div className="flex flex-col gap-3">
			{profiles.map((profile, index) => (
				<div className="grid grid-cols-2 gap-2 sm:grid-cols-4" key={index}>
					<div className="flex flex-col gap-1">
						<Label htmlFor={`profile-${index}-label`}>{t("coordinators.profileLabel")}</Label>
						<Input disabled={disabled} id={`profile-${index}-label`} onChange={(event) => update(index, { label: event.target.value })} value={profile.label} />
					</div>
					<div className="flex flex-col gap-1">
						<Label htmlFor={`profile-${index}-id`}>{t("coordinators.profileId")}</Label>
						<Input disabled={disabled} id={`profile-${index}-id`} onChange={(event) => update(index, { id: event.target.value })} value={profile.id} />
					</div>
					<div className="flex flex-col gap-1">
						<Label htmlFor={`profile-${index}-harness`}>{t("coordinators.profileHarness")}</Label>
						<Input disabled={disabled} id={`profile-${index}-harness`} onChange={(event) => update(index, { harness: event.target.value })} value={profile.harness} />
					</div>
					<div className="flex flex-col gap-1">
						<Label htmlFor={`profile-${index}-model`}>{t("coordinators.profileModel")}</Label>
						<Input disabled={disabled} id={`profile-${index}-model`} onChange={(event) => update(index, { model: event.target.value })} value={profile.model} />
					</div>
					<div className="flex flex-col gap-1">
						<Label htmlFor={`profile-${index}-effort`}>{t("coordinators.profileEffort")}</Label>
						<Input disabled={disabled} id={`profile-${index}-effort`} onChange={(event) => update(index, { effort: event.target.value })} value={profile.effort} />
					</div>
					<div className="flex flex-col gap-1">
						<Label htmlFor={`profile-${index}-weight`}>{t("coordinators.profileWeight")}</Label>
						<Input disabled={disabled} id={`profile-${index}-weight`} inputMode="numeric" onChange={(event) => update(index, { weight: event.target.value })} value={profile.weight} />
					</div>
					<div className="flex items-end">
						<Button disabled={disabled} onClick={() => onChange(profiles.filter((_, position) => position !== index))} size="sm" type="button" variant="ghost">
							{t("coordinators.profileRemove")}
						</Button>
					</div>
				</div>
			))}
			{error ? <p className="text-sm text-error" role="alert">{t(error)}</p> : null}
			<div>
				<Button disabled={disabled} onClick={() => onChange([...profiles, emptyProfile()])} size="sm" type="button" variant="outline">
					{t("coordinators.profileAdd")}
				</Button>
			</div>
		</div>
	);
}
