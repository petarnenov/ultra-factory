import type { AoBridge } from "../../preload";
import type { DaemonStatus } from "../../shared/daemon-status";
import { isWebBuild } from "./platform";
import { usesPreviewWorkspaceData } from "./preview-mode";
import { isHubSignedOut, redirectToHubLogin } from "./hub-login";
import { createWebNotificationsBridge } from "./web-notifications";
import { createWebRemotesBridge } from "./web-remotes";
import { webKeybindings, webUiSettings } from "./web-settings";
import { setWebShortcutRecording, webShortcutListeners } from "./web-shortcuts";
export type { FeatureBuild } from "../../main/feature-builds";

// The web build has no Electron supervisor: the daemon (or the proxy in front
// of it) answers on the API base URL, so readiness comes from its /readyz.
const WEB_DAEMON_POLL_MS = 5_000;
const WEB_DAEMON_READY_POLL_MS = 30_000;

function webDaemonBaseUrl(): string {
	return (import.meta.env.VITE_AO_API_BASE_URL ?? "").replace(/\/+$/, "");
}

// The shell only trusts a ready status that carries a port. A proxy on the
// scheme's default port leaves the URL port empty, so report the effective one.
function webDaemonPort(baseUrl: string): number {
	const url = new URL(baseUrl || window.location.href, window.location.href);
	if (url.port) return Number(url.port);
	return url.protocol === "https:" ? 443 : 80;
}

function webDaemonIsSameOrigin(baseUrl: string): boolean {
	return new URL(baseUrl || window.location.href, window.location.href).origin === window.location.origin;
}

async function probeWebDaemon(): Promise<DaemonStatus> {
	const baseUrl = webDaemonBaseUrl();
	try {
		const response = await fetch(`${baseUrl}/readyz`, { cache: "no-store" });
		// Only the page's own origin has the hub's /login.
		if (response.status === 401 && webDaemonIsSameOrigin(baseUrl) && isHubSignedOut(401, await response.json().catch(() => null))) {
			redirectToHubLogin();
			return { state: "error", message: "The hub session has ended. Sign in again." };
		}
		if (response.status !== 200) {
			return { state: "error", message: `Daemon readiness check returned HTTP ${response.status}.` };
		}
		// A static host's SPA fallback also answers 200, with index.html.
		const body = (await response.json().catch(() => null)) as { status?: unknown } | null;
		if (body?.status !== "ready") {
			return { state: "error", message: "Daemon readiness check did not return a ready daemon." };
		}
		return { state: "ready", port: webDaemonPort(baseUrl) };
	} catch (error) {
		return {
			state: "error",
			message: `Daemon readiness check failed: ${error instanceof Error ? error.message : String(error)}`,
		};
	}
}

async function unsupportedWebDaemonControl(): Promise<DaemonStatus> {
	return { ...(await probeWebDaemon()), supported: false };
}

// Browsers allow clipboard reads only in a secure context, after user gesture.
// A denial is toasted and resolves to empty text instead of failing the caller.
async function readWebClipboardText(): Promise<string> {
	try {
		if (!navigator.clipboard?.readText) throw new Error("Clipboard API is unavailable");
		return await navigator.clipboard.readText();
	} catch {
		const { useUiStore } = await import("../stores/ui-store");
		useUiStore.getState().showGlobalToast(
			"Clipboard access is blocked",
			"Allow clipboard access for this site, or paste with your browser's paste shortcut.",
			{ tone: "info", dedupeKey: "web-clipboard-read-denied" },
		);
		return "";
	}
}

type WebDaemonListener = Parameters<AoBridge["daemon"]["onStatus"]>[0];

// Every onStatus subscriber (status hook, transports, failure telemetry)
// shares one /readyz loop: a loop each multiplied the probes in every tab.
const webDaemonListeners = new Set<WebDaemonListener>();
let webDaemonTimer: ReturnType<typeof setTimeout> | undefined;
let webDaemonLoop = 0;

function scheduleWebDaemonProbe(delayMs: number): void {
	const loop = webDaemonLoop;
	webDaemonTimer = setTimeout(() => {
		void probeWebDaemon().then((status) => {
			if (loop !== webDaemonLoop) return;
			for (const listener of [...webDaemonListeners]) {
				// An earlier listener may have unsubscribed this one.
				if (!webDaemonListeners.has(listener)) continue;
				// One failing subscriber must not stop the probe for the others; its
				// error still surfaces as an uncaught one.
				try {
					listener(status);
				} catch (error) {
					queueMicrotask(() => { throw error; });
				}
			}
			if (loop !== webDaemonLoop) return;
			scheduleWebDaemonProbe(status.state === "ready" ? WEB_DAEMON_READY_POLL_MS : WEB_DAEMON_POLL_MS);
		});
	}, delayMs);
}

const webDaemonBridge: AoBridge["daemon"] = {
	getStatus: probeWebDaemon,
	start: unsupportedWebDaemonControl,
	stop: unsupportedWebDaemonControl,
	restart: unsupportedWebDaemonControl,
	onStatus: (listener) => {
		// A wrapper keeps two subscriptions of the same function independent.
		const entry: WebDaemonListener = (status) => listener(status);
		webDaemonListeners.add(entry);
		if (webDaemonListeners.size === 1) scheduleWebDaemonProbe(WEB_DAEMON_POLL_MS);
		return () => {
			if (!webDaemonListeners.delete(entry) || webDaemonListeners.size > 0) return;
			// The last subscriber left: stop the loop, and let an in-flight probe
			// see that it belongs to a finished loop.
			webDaemonLoop += 1;
			clearTimeout(webDaemonTimer);
			webDaemonTimer = undefined;
		};
	},
};

export const aoBridge: AoBridge =
	window.ao ??
	({
		app: {
			getVersion: async () => "0.0.0-preview",
			chooseDirectory: async () => null,
			checkGitRepository: async () => true,
			openExternal: async (url: string) => {
				window.open(url, "_blank", "noopener,noreferrer");
			},
			scanImportFolder: async ({ path }) => ({ path, repos: [] }),
			checkAncestorRepo: async () => undefined,
		getRepositoryBranch: async () => undefined,
		getGitHubLogin: async (_repoPath?: string) => "",
		getCachedGitHubOwners: async () => [],
		refreshGitHubOwners: async () => [],
		checkGitHubRepositoryAvailability: async () => ({ available: true }),
			getPathForFile: () => "",
			onOpenFolderPath: () => () => undefined,
			// With no main process, shortcuts come from the keydown handler in
			// web-shortcuts.ts, which is installed only when isWebBuild.
			...webShortcutListeners,
			setChatDraftRisk: () => undefined,
		},
		terminal: {
			saveDroppedFile: async () => "",
			setFocused: () => undefined,
			onFontSizeShortcut: () => () => undefined,
		},
		window: {
			getZoomFactor: async () => 1,
			onZoomFactor: () => () => undefined,
			isMaximized: async () => false,
			onMaximized: () => () => undefined,
			isFullScreen: async () => false,
			onFullScreen: () => () => undefined,
		},
		theme: {
			set: async () => undefined,
			persistTerminal: async () => undefined,
			setWindowBackground: async () => undefined,
			setWindowTranslucent: async () => undefined,
		},
		menu: {
			action: async () => undefined,
			notifyShellFocus: () => undefined,
		},
		clipboard: {
			writeText: async (text: string) => {
				if (navigator.clipboard?.writeText) {
					await navigator.clipboard.writeText(text);
				}
			},
			readText: readWebClipboardText,
		},
		// Mock-data preview (VITE_NO_ELECTRON) has no daemon to probe.
		daemon: usesPreviewWorkspaceData
			? {
				getStatus: async () => ({
					state: "stopped",
					message: "Electron preload is not available in browser preview.",
				}),
				start: async () => ({ state: "starting" }),
				stop: async () => ({ state: "stopped" }),
				restart: async () => ({ state: "starting" }),
				onStatus: () => () => undefined,
			}
			: webDaemonBridge,
		sideChats: { capture: async () => undefined },
		editorHandoff: {
			getState: async () => ({
				targets: [],
				preferredEditorId: "cursor",
				workspaceAvailable: false,
				unavailableReason: "Desktop app is required to open a workspace.",
			}),
			open: async () => {
				throw new Error("Desktop app is required to open a workspace.");
			},
			openArtifacts: async () => {
				throw new Error("Desktop app is required to open artifacts.");
			},
			copyWorkspacePath: async () => {
				throw new Error("Desktop app is required to copy a workspace path.");
			},
		},
		telemetry: {
			getBootstrap: async () => null,
			getPolicy: async () => ({ eventsEnabled: false, consentGeneration: "preview", updatedAt: new Date(0).toISOString(), acknowledged: false, consentRenewalRequired: false, state: "applied", environmentVeto: true, durabilitySupported: false, reason: "environment_veto" }),
			setEventsEnabled: async () => ({ eventsEnabled: false, consentGeneration: "preview", updatedAt: new Date(0).toISOString(), acknowledged: false, consentRenewalRequired: false, state: "applied", environmentVeto: true, durabilitySupported: false, reason: "environment_veto" }),
			onPolicy: () => () => false,
			onClearQueues: () => () => false,
			capture: async () => false,
			signalAgentSwitchVisibility: () => false,
		},
		browser: {
			nativeCompositionEnabled: false,
			reconnectRuntime: async () => undefined,
			getRuntimeState: async () => ({ connected: false }),
			ensure: async (sessionId: string) => ({
				viewId: `preview:${sessionId}`,
				url: "",
				title: "",
				canGoBack: false,
				canGoForward: false,
				isLoading: false,
			}),
			setBounds: () => undefined,
			onBoundsApplied: () => () => undefined,
			setOverlayOpen: () => undefined,
			navigate: async ({ viewId, url }) => ({
				viewId,
				url,
				title: "",
				canGoBack: false,
				canGoForward: false,
				isLoading: false,
			}),
			historySuggestions: async () => [],
			historyFavicon: async () => undefined,
			clear: async (viewId: string) => ({
				viewId,
				url: "",
				title: "",
				canGoBack: false,
				canGoForward: false,
				isLoading: false,
			}),
			goBack: async (viewId: string) => ({
				viewId,
				url: "",
				title: "",
				canGoBack: false,
				canGoForward: false,
				isLoading: false,
			}),
			goForward: async (viewId: string) => ({
				viewId,
				url: "",
				title: "",
				canGoBack: false,
				canGoForward: false,
				isLoading: false,
			}),
			reload: async (viewId: string) => ({
				viewId,
				url: "",
				title: "",
				canGoBack: false,
				canGoForward: false,
				isLoading: false,
			}),
			stop: async (viewId: string) => ({
				viewId,
				url: "",
				title: "",
				canGoBack: false,
				canGoForward: false,
				isLoading: false,
			}),
			getFindState: async (viewId: string) => ({
				viewId,
				tabId: "t1",
				query: "",
				activeMatchOrdinal: 0,
				matches: 0,
				finalUpdate: true,
			}),
			findInPage: async ({ viewId, query }) => ({
				viewId,
				tabId: "t1",
				query,
				activeMatchOrdinal: 0,
				matches: 0,
				finalUpdate: true,
			}),
			stopFindInPage: async ({ viewId }) => ({
				viewId,
				tabId: "t1",
				query: "",
				activeMatchOrdinal: 0,
				matches: 0,
				finalUpdate: true,
			}),
			captureScreenshot: async () => {
				throw new Error("Desktop app is required to take a browser screenshot.");
			},
			downloads: {
				list: async () => ({ downloads: [] }),
				action: async () => ({ downloads: [] }),
				clear: async () => ({ downloads: [] }),
				onChanged: () => () => { /* preview has no native download events */ },
			},
			getTabs: async (viewId: string) => ({ viewId, activeTabId: "t1", tabs: [] }),
			selectTab: async ({ viewId, tabId }) => ({ viewId, activeTabId: tabId, tabs: [] }),
			closeTab: async ({ viewId }) => ({ viewId, activeTabId: "", tabs: [] }),
			openTab: async ({ viewId }) => ({ viewId, activeTabId: "", tabs: [] }),
			getProfile: async (viewId) => ({ viewId, profileId: null, temporary: true }),
			showProfileMenu: async () => undefined,
			selectProfile: async () => undefined,
			notifyPanelUsed: () => undefined,
			notifyPanelBlur: () => undefined,
			onFocusLocation: () => () => undefined,
			onFindOpen: () => () => undefined,
			onReopenClosedTab: () => () => undefined,
			onClosePanel: () => () => undefined,
			devtools: async ({ viewId, operation }) => ({
				viewId,
				open: operation !== "close",
				activeTabId: "",
			}),
			destroy: () => undefined,
			setAnnotationMode: async () => undefined,
			completeAnnotation: async () => undefined,
			discardAnnotations: async () => undefined,
			annotationAction: async () => undefined,
			onNavState: () => () => undefined,
			onFindState: () => () => undefined,
			onPageFocus: () => () => undefined,
			onTabsState: () => () => undefined,
			onAgentActivity: () => () => undefined,
			onRuntimeState: () => () => undefined,
			onDevToolsState: () => () => undefined,
			onProfileState: () => () => undefined,
			onProfileManage: () => () => undefined,
			onAnnotationSubmit: () => () => undefined,
			onAnnotationCancel: () => () => undefined,
			onAnnotationState: () => () => undefined,
		},
		browserProfiles: {
			list: async () => ({ profiles: [], defaultProfileId: null }),
			create: async (name: string) => {
				const now = new Date().toISOString();
				return { id: `preview-${name}`, name, createdAt: now, updatedAt: now };
			},
			rename: async ({ id, name }) => {
				const now = new Date().toISOString();
				return { id, name, createdAt: now, updatedAt: now };
			},
			clear: async () => undefined,
			delete: async () => undefined,
			setDefault: async () => undefined,
			discoverImportSources: async () => ({ sources: [] }),
			import: async () => ({ sourceName: "", entries: [] }),
			onImportProgress: () => () => undefined,
		},
		// Mock-data preview (VITE_NO_ELECTRON) stays without notifications; the web
		// build uses Web Notifications, a document.title badge and the bundled sound.
		notifications: usesPreviewWorkspaceData
			? {
				show: async () => undefined,
				setBadge: async () => undefined,
				devBounce: async () => undefined,
				onClick: () => () => undefined,
				onPlaySound: () => () => undefined,
				reportSoundFailure: () => undefined,
			}
			: createWebNotificationsBridge({
				// Dynamic import: the store imports aoBridge from this module.
				soundEnabled: async () =>
					(await import("../stores/sound-notifications-store")).useSoundNotificationsStore.getState().enabled,
			}),
		tray: {
			setAttentionState: () => undefined,
			onOpenSession: () => () => undefined,
		},
		appState: {
			getMigration: async () => ({ status: "pending" }),
			setMigration: async () => undefined,
		},
		cliLink: {
			setEnabled: async () => undefined,
			inspect: async () => ({ ok: false as const, code: "unsupported" as const, message: "The desktop app is required to link the global ao command." }),
			link: async () => ({ ok: false as const, code: "unsupported" as const, message: "The desktop app is required to link the global ao command." }),
			unlink: async () => ({ ok: false as const, code: "unsupported" as const, message: "The desktop app is required to link the global ao command." }),
		},
		updateSettings: {
			get: async () => ({ enabled: false, channel: "latest", nightlyAck: false, feature: null, macDifferentialUpdates: false }),
			set: async () => undefined,
			setMacDifferentialUpdates: async () => undefined,
		},
		uiSettings: webUiSettings,
		keybindings: {
			...webKeybindings,
			setRecording: async (active) => setWebShortcutRecording(active),
		},
		updates: {
			getStatus: async () => ({ state: "idle" }),
			check: async () => undefined,
			returnHome: async () => undefined,
			download: async () => undefined,
			install: async () => undefined,
			isPostUpdateRelaunch: async () => false,
			relaunch: async () => undefined,
			onStatus: () => () => undefined,
			onTelemetry: () => () => undefined,
		},
		featureBuilds: {
			list: async () => [],
			getActive: async () => null,
		},
		// The web build reads the hub BFF's host registry; the mock preview has
		// no BFF and no ~/.ao/remotes.json, so it reports none and shows local.
		remotes: isWebBuild ? createWebRemotesBridge(webDaemonBaseUrl) : {
			list: async () => [],
			importAccountHost: async () => undefined,
			pruneAccountHosts: async () => undefined,
			issueAccountToken: async () => { throw new Error("remote hosts need the desktop app"); },
			add: async () => "offline" as const,
			update: async () => "offline" as const,
			remove: async () => undefined,
			connect: async () => { throw new Error("remote hosts need the desktop app"); },
			disconnect: async () => undefined,
			previewUrl: async (_hostId: string, _sessionId: string, sourceUrl: string) => sourceUrl,
			resolvePreviewUrl: async (_hostId: string, _sessionId: string, viewedUrl: string) => viewedUrl,
		},
		cloud: {
			getSession: async () => null,
			signIn: async () => undefined,
			signOut: async () => undefined,
			cancelProviderAuth: async () => undefined,
			connectProviderAuth: async () => {
				throw new Error("Cloud provider sign-in requires the desktop app.");
			},
			localAuthAvailable: async () => false,
			localRegister: async () => {
				throw new Error("AO Cloud sign-in requires the desktop app.");
			},
			localLogin: async () => {
				throw new Error("AO Cloud sign-in requires the desktop app.");
			},
			onSessionChanged: () => () => undefined,
		},
		cloudCp: {
			request: async () => {
				throw new Error("AO Cloud requests require the desktop app.");
			},
			openStream: async () => {
				throw new Error("AO Cloud event streams require the desktop app.");
			},
			closeStream: () => undefined,
			onStreamEvent: () => () => undefined,
		},
	} satisfies AoBridge);
