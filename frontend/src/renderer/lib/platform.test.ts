import { afterEach, describe, expect, it, vi } from "vitest";
import {
	isLinuxPlatform,
	isMacPlatform,
	isWindowsPlatform,
	usesFramedAppTopbar,
	hidesShellTopbar,
	usesBoardActionsInPanel,
	resolveCapabilities,
} from "./platform";

const originalPlatform = Object.getOwnPropertyDescriptor(window.navigator, "platform");
const originalUserAgent = Object.getOwnPropertyDescriptor(window.navigator, "userAgent");
const originalUserAgentData = Object.getOwnPropertyDescriptor(window.navigator, "userAgentData");

function spoofPlatform(platform: string, userAgent = platform) {
	Object.defineProperty(window.navigator, "platform", {
		configurable: true,
		get: () => platform,
	});
	Object.defineProperty(window.navigator, "userAgent", {
		configurable: true,
		get: () => userAgent,
	});
	Object.defineProperty(window.navigator, "userAgentData", {
		configurable: true,
		get: () => ({ platform }),
	});
}

function restoreProperty(name: "platform" | "userAgent" | "userAgentData", descriptor: PropertyDescriptor | undefined) {
	if (descriptor) {
		Object.defineProperty(window.navigator, name, descriptor);
		return;
	}
	delete (window.navigator as unknown as Record<string, unknown>)[name];
}

afterEach(() => {
	restoreProperty("platform", originalPlatform);
	restoreProperty("userAgent", originalUserAgent);
	restoreProperty("userAgentData", originalUserAgentData);
});

describe("renderer platform behavior", () => {
	it("hides the shell topbar on macOS and keeps board actions in the panel", () => {
		spoofPlatform("MacIntel", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7)");

		expect(isMacPlatform()).toBe(true);
		expect(isWindowsPlatform()).toBe(false);
		expect(usesFramedAppTopbar()).toBe(true);
		expect(hidesShellTopbar()).toBe(true);
		expect(usesBoardActionsInPanel()).toBe(true);
	});

	it("keeps Windows board controls in the inset panel topbar", () => {
		spoofPlatform("Win32");

		expect(isWindowsPlatform()).toBe(true);
		expect(isMacPlatform()).toBe(false);
		expect(usesFramedAppTopbar()).toBe(true);
		expect(hidesShellTopbar()).toBe(false);
		expect(usesBoardActionsInPanel()).toBe(false);
	});

	it("shows the shell topbar on Linux (same as Windows, not macOS)", () => {
		spoofPlatform("Linux x86_64");

		expect(isLinuxPlatform()).toBe(true);
		expect(isWindowsPlatform()).toBe(false);
		expect(usesFramedAppTopbar()).toBe(true);
		expect(hidesShellTopbar()).toBe(false);
		expect(usesBoardActionsInPanel()).toBe(false);
	});
});

describe("renderer capabilities", () => {
	afterEach(() => {
		vi.unstubAllEnvs();
		vi.resetModules();
	});

	it("enables every desktop capability in the Electron renderer", () => {
		const caps = resolveCapabilities({ webBuild: false, electronBridge: true });

		expect(caps).toEqual({
			browserPanel: true,
			editorHandoff: true,
			cliLink: true,
			autoUpdate: true,
			nativeDialogs: true,
			tray: true,
			windowChrome: true,
		});
		expect(Object.isFrozen(caps)).toBe(true);
	});

	it("disables every desktop capability in the web build", () => {
		const caps = resolveCapabilities({ webBuild: true, electronBridge: false });

		expect(Object.values(caps).every((enabled) => !enabled)).toBe(true);
	});

	it("disables desktop capabilities without the preload bridge", () => {
		const caps = resolveCapabilities({ webBuild: false, electronBridge: false });

		expect(Object.values(caps).some(Boolean)).toBe(false);
	});

	it("treats the web build as web-only even if a bridge is present", () => {
		const caps = resolveCapabilities({ webBuild: true, electronBridge: true });

		expect(Object.values(caps).some(Boolean)).toBe(false);
	});

	it("reads the Electron flags from the test renderer", async () => {
		vi.resetModules();
		const platform = await import("./platform");

		expect(platform.isWebBuild).toBe(false);
		expect(platform.hasElectronBridge).toBe(true);
		expect(platform.capabilities.browserPanel).toBe(true);
	});

	it("reads VITE_AO_WEB=1 as the web build with no desktop capabilities", async () => {
		vi.stubEnv("VITE_AO_WEB", "1");
		const original = window.ao;
		window.ao = undefined;
		try {
			vi.resetModules();
			const platform = await import("./platform");

			expect(platform.isWebBuild).toBe(true);
			expect(platform.hasElectronBridge).toBe(false);
			expect(Object.values(platform.capabilities).some(Boolean)).toBe(false);
		} finally {
			window.ao = original;
		}
	});
});
