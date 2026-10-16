import type { AoBridge } from "../../preload";
import { shouldSignalAttention } from "../../main/notification-signals";

// Браузърният еквивалент на notifications групата, която в Electron държи
// main процесът (main.ts, notifications:show/setBadge). Тук няма dock, tray и
// системен beep: toast-ът е Web Notification, badge-ът е префикс в
// document.title, а звукът минава през същите onPlaySound слушатели, които
// main.tsx вече закача към бъндълнатия notification.mp3.

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
 * Пита браузъра за разрешение. Вика се само от потребителско действие (бутона в
 * Settings): браузърите блокират или игнорират заявка без user gesture, а
 * prompt при зареждане е натрапчив, затова show() никога не пита сам.
 */
export async function requestWebNotificationPermission(): Promise<WebNotificationPermission> {
	const api = notificationApi();
	if (!api) return "unsupported";
	if (api.permission !== "default") return api.permission;
	return api.requestPermission();
}

/** Слага или маха префикса `(n) ` и запазва останалата част от заглавието. */
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
			// Както в main: „вече гледа“ = прозорецът е на фокус или промптът е на
			// екрана (`watched`). Тогава няма визуален сигнал и звук.
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
					// Същото id заменя предишния toast вместо да трупа дубликати.
					tag: notification.id,
				});
				toast.onclick = () => {
					window.focus();
					toast.close();
					for (const listener of clickListeners) listener(notification.id);
				};
			} catch {
				// Някои браузъри (Android Chrome) позволяват Notification само от
				// service worker; липсващ toast не бива да чупи UI-а.
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
		// Браузърът няма системен beep, към който да падне.
		reportSoundFailure: () => undefined,
	};
}
