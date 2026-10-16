import { electronBridge } from "./electron-bridge";
import { usesPreviewWorkspaceData } from "./preview-mode";

type NavigatorWithUserAgentData = Navigator & { userAgentData?: { platform?: string } };

function navigatorPlatform(): string {
	if (typeof navigator === "undefined") return "";
	return (navigator as NavigatorWithUserAgentData).userAgentData?.platform ?? navigator.platform ?? "";
}

function navigatorUserAgent(): string {
	if (typeof navigator === "undefined") return "";
	return navigator.userAgent ?? "";
}

export function isMacPlatform(): boolean {
	return /Mac|iPod|iPhone|iPad/.test(navigatorUserAgent()) || /mac/i.test(navigatorPlatform());
}

export function isWindowsPlatform(): boolean {
	return /win/i.test(navigatorPlatform());
}

export function isLinuxPlatform(): boolean {
	return navigatorPlatform().toLowerCase().includes("linux");
}

export function usesFramedAppTopbar(): boolean {
	// Keep this behind a helper even while every desktop platform uses the
	// framed shell: it leaves the legacy full-width topbar path explicit while
	// the cross-platform chrome settles, and keeps platform-specific tests at
	// the behavior boundary instead of scattered through route/components code.
	return true;
}

/**
 * macOS only: shell does not mount ShellTopbar (full-height inset panel).
 * The sidebar toggle + history arrows live in the fixed TitlebarNav cluster and
 * board/session actions mount in-panel, with a traffic-light drag strip for
 * window movement. Win/Linux keep the ShellTopbar spanning the window, with the
 * sidebar hanging below it — two different ways of creating the top gap.
 *
 * Note: Linux previously returned true here (taking the macOS path), which was
 * a bug — it left Linux with no ShellTopbar and a 6px mac inset that masked
 * y=0 panel content. The DESIGN.md 2026-06-12 divergence doc always specified
 * Win/Linux as the ShellTopbar path. Fixed in PR #3421.
 */
export function hidesShellTopbar(): boolean {
	// Web build-ът няма светофар на macOS и ползва Linux shell-а на всяка ОС.
	return isMacPlatform() && !isWebBuild;
}

/**
 * Board New task / Orchestrator / bell render in the board body instead of the
 * framed shell topbar (macOS only). Win/Linux keep those controls in the topbar.
 *
 * This flipped for Linux as part of fixing hidesShellTopbar() — board actions
 * now live in the visible ShellTopbar on Linux, matching Windows behavior.
 */
export function usesBoardActionsInPanel(): boolean {
	return hidesShellTopbar();
}

// VITE_AO_WEB=1 selects the standalone web renderer (build:web, dev:web:real).
export const isWebBuild = import.meta.env.VITE_AO_WEB === "1";

// True when the Electron preload installed its bridge. False in the web build
// and in the VITE_NO_ELECTRON mock preview.
export const hasElectronBridge = electronBridge() !== undefined;

export type Capability =
	| "browserPanel"
	| "editorHandoff"
	| "cliLink"
	| "autoUpdate"
	| "nativeDialogs"
	| "tray"
	| "windowChrome";

export function resolveCapabilities(options: { webBuild: boolean; electronBridge: boolean }): Readonly<Record<Capability, boolean>> {
	// Every capability here needs the Electron supervisor, which the web build
	// never has. They are listed separately so callers gate on the feature, not
	// the build, and a later web implementation can flip one at a time.
	const desktop = options.electronBridge && !options.webBuild;
	return Object.freeze({
		browserPanel: desktop,
		editorHandoff: desktop,
		cliLink: desktop,
		autoUpdate: desktop,
		nativeDialogs: desktop,
		tray: desktop,
		windowChrome: desktop,
	});
}

// Mock preview-ът (VITE_NO_ELECTRON) показва desktop UI-я: stub-овете в bridge.ts
// заместват preload-а, а e2e тестовете минават през desktop повърхностите в него.
// Затова той запазва desktop capabilities; само web build-ът ги изключва.
export const capabilities = resolveCapabilities({ webBuild: isWebBuild, electronBridge: hasElectronBridge || usesPreviewWorkspaceData });
