import type * as Platform from "../lib/platform";

/**
 * Модулът lib/platform такъв, какъвто го вижда web build-ът (VITE_AO_WEB=1 без
 * Electron preload). Тестовете на скритите повърхности го подават на vi.mock:
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
