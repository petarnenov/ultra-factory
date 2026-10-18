import { useState } from "react";
import { useTranslation } from "react-i18next";
import { isWebBuild } from "../../lib/platform";
import {
	requestWebNotificationPermission,
	webNotificationPermission,
	type WebNotificationPermission,
} from "../../lib/web-notifications";
import { Button } from "../ui/button";
import { SettingsRow } from "./SettingsRow";

/** Browser notification permission: only this button asks, never on load.
 * In Electron the row is not shown. */
export function WebNotificationsRow({ webBuild = isWebBuild }: { webBuild?: boolean }) {
	const { t } = useTranslation();
	const [permission, setPermission] = useState<WebNotificationPermission>(() => webNotificationPermission());
	const [requesting, setRequesting] = useState(false);

	if (!webBuild || permission === "unsupported") return null;

	const description =
		permission === "denied" ? t("settings.webNotifications.blocked") : t("settings.webNotifications.description");

	return (
		<SettingsRow label={t("settings.webNotifications")} description={description}>
			{permission === "default" ? (
				<Button
					type="button"
					variant="footer"
					disabled={requesting}
					onClick={() => {
						setRequesting(true);
						void requestWebNotificationPermission()
							.then(setPermission)
							.catch(() => setPermission(webNotificationPermission()))
							.finally(() => setRequesting(false));
					}}
				>
					{t("settings.webNotifications.allow")}
				</Button>
			) : (
				<Button type="button" variant="footer" disabled>
					{permission === "granted" ? t("settings.webNotifications.allowed") : t("settings.webNotifications.blockedState")}
				</Button>
			)}
		</SettingsRow>
	);
}
