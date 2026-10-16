import type { AoBridge } from "../../preload";
import { shouldSignalAttention } from "../../main/notification-signals";

// Browser counterpart of the notifications group that the Electron main process
// owns (main.ts, notifications:show/setBadge). There is no dock, tray or system
// beep here: the toast is a Web Notification, the badge is a document.title
// prefix, and the sound goes through the same onPlaySound listeners that
// main.tsx already wires to the bundled notification.mp3.

export type WebNotificationPermission = NotificationPermission | "unsupported";

type NotificationShowInput = Parameters<AoBridge["notifications"]["show"]>[0];

const BADGE_PREFIX = /^\(\d+\) /;

function notificationApi(): typeof Notification | undefined {
	return typeof Notification === "undefined" ? undefined : Notification;
}

export function webNotificationPermission(): WebNotificationPermission {
	return notificationApi()?.permission ?? "unsupported";
}

/**
 * Asks the browser for permission. Only call it from a user action (the
 * Settings button): browsers block or ignore requests without a user gesture,
 * and a prompt on load is intrusive, so show() never asks on its own.
 */
export async function requestWebNotificationPermission(): Promise<WebNotificationPermission> {
	const api = notificationApi();
	if (!api) return "unsupported";
	if (api.permission !== "default") return api.permission;
	return api.requestPermission();
}

/** Adds or removes the `(n) ` prefix and keeps the rest of the title. */
export function badgedTitle(title: string, count: number): string {
	const base = title.replace(BADGE_PREFIX, "");
	const n = Number.isFinite(count) && count > 0 ? Math.floor(count) : 0;
	return n > 0 ? `(${n}) ${base}` : base;
}

export function createWebNotificationsBridge(options: {
	soundEnabled: () => Promise<boolean>;
}): AoBridge["notifications"] {
	const clickListeners = new Set<(id: string) => void>();
	const soundListeners = new Set<() => void>();

	const playSound = () => {
		for (const listener of soundListeners) listener();
	};

	return {
		show: async (notification: NotificationShowInput) => {
			if (!notification.id) return;
			// As in main: "already looking" = the window has focus or the prompt is
			// on screen (`watched`). No visual signal or sound then.
			const looking = (document.hasFocus() && document.visibilityState === "visible") || notification.watched === true;
			if (looking) return;
			if (shouldSignalAttention(notification.type)) {
				const enabled = await options.soundEnabled().catch(() => true);
				if (enabled) playSound();
			}
			const api = notificationApi();
			if (!api || api.permission !== "granted" || !notification.title) return;
			try {
				const toast = new api(notification.title, {
					body: notification.body,
					// The same id replaces the previous toast instead of stacking duplicates.
					tag: notification.id,
				});
				toast.onclick = () => {
					window.focus();
					toast.close();
					for (const listener of clickListeners) listener(notification.id);
				};
			} catch {
				// Some browsers (Android Chrome) only allow Notification from a
				// service worker; a missing toast must not break the UI.
			}
		},
		setBadge: async (count: number) => {
			const next = badgedTitle(document.title, count);
			if (next !== document.title) document.title = next;
		},
		devBounce: async () => {
			if (await options.soundEnabled().catch(() => true)) playSound();
		},
		onClick: (listener) => {
			clickListeners.add(listener);
			return () => {
				clickListeners.delete(listener);
			};
		},
		onPlaySound: (listener) => {
			soundListeners.add(listener);
			return () => {
				soundListeners.delete(listener);
			};
		},
		// The browser has no system beep to fall back to.
		reportSoundFailure: () => undefined,
	};
}
