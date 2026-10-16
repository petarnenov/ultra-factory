import type * as Platform from "../lib/platform";

/**
 * The lib/platform module as the web build sees it (VITE_AO_WEB=1, no Electron
 * preload). Tests for the hidden surfaces pass it to vi.mock:
 *
 *   vi.mock("../lib/platform", async (importOriginal) =>
 *     (await import("../test/web-build-platform")).webBuildPlatform(await importOriginal()));
 */
export function webBuildPlatform(actual: typeof Platform): typeof Platform {
	return {
		...actual,
		isWebBuild: true,
		hasElectronBridge: false,
		capabilities: actual.resolveCapabilities({ webBuild: true, electronBridge: false }),
		hidesShellTopbar: () => false,
		usesBoardActionsInPanel: () => false,
	};
}
