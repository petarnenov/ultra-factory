import { apiClient, apiErrorMessage } from "./api-client";
import { aoBridge } from "./bridge";
import { capabilities } from "./platform";

/**
 * Копира пътя на workspace-а на локална сесия в клипборда. Desktop приложението
 * минава през Electron main (editor handoff); web build-ът няма editor handoff,
 * затова пита daemon-а за пътя и го записва през клипборда на браузъра.
 */
export async function copySessionWorkspacePath(sessionId: string): Promise<void> {
	if (capabilities.editorHandoff) {
		await aoBridge.editorHandoff.copyWorkspacePath(sessionId);
		return;
	}
	const { data, error } = await apiClient.GET("/api/v1/desktop/sessions/{sessionId}/workspace", {
		params: { path: { sessionId } },
	});
	if (error || !data?.workspacePath) throw new Error(apiErrorMessage(error, "Session workspace is not available."));
	if (!navigator.clipboard?.writeText) throw new Error("Clipboard is not available.");
	await navigator.clipboard.writeText(data.workspacePath);
}
