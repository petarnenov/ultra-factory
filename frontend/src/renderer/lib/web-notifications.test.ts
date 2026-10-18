import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
	badgedTitle,
	createWebNotificationsBridge,
	requestWebNotificationPermission,
	webNotificationPermission,
} from "./web-notifications";

type ToastInstance = { title: string; options?: NotificationOptions; onclick: (() => void) | null; close: ReturnType<typeof vi.fn> };

const toasts: ToastInstance[] = [];

// Web Notifications API mock: records every new Notification(...) and lets the
// test set the permission and the requestPermission answer.
function installNotificationMock(permission: NotificationPermission, answer: NotificationPermission = "granted") {
	const MockNotification = vi.fn(function (this: ToastInstance, title: string, options?: NotificationOptions) {
		this.title = title;
		this.options = options;
		this.onclick = null;
		this.close = vi.fn();
		toasts.push(this);
	}) as unknown as typeof Notification & { permission: NotificationPermission; requestPermission: ReturnType<typeof vi.fn> };
	MockNotification.permission = permission;
	MockNotification.requestPermission = vi.fn(async () => {
		MockNotification.permission = answer;
		return answer;
	});
	vi.stubGlobal("Notification", MockNotification);
	return MockNotification;
}

function setLooking(looking: boolean) {
	vi.spyOn(document, "hasFocus").mockReturnValue(looking);
}

beforeEach(() => {
	toasts.length = 0;
	document.title = "AO";
});

afterEach(() => {
	vi.restoreAllMocks();
	vi.unstubAllGlobals();
});

describe("web notifications bridge", () => {
	it("shows a Notification for a granted permission while the tab is in the background", async () => {
		installNotificationMock("granted");
		setLooking(false);
		const bridge = createWebNotificationsBridge({ soundEnabled: async () => true });

		await bridge.show({ id: "n1", title: "Agent needs your input", body: "ao-7", type: "needs_input" });

		expect(toasts).toHaveLength(1);
		expect(toasts[0].title).toBe("Agent needs your input");
		expect(toasts[0].options).toEqual({ body: "ao-7", tag: "n1" });
	});

	it("never asks for permission on its own and stays silent until granted", async () => {
		const api = installNotificationMock("default");
		setLooking(false);
		const bridge = createWebNotificationsBridge({ soundEnabled: async () => true });

		await bridge.show({ id: "n1", title: "Agent needs your input", type: "needs_input" });

		expect(api.requestPermission).not.toHaveBeenCalled();
		expect(toasts).toHaveLength(0);
	});

	it("skips the toast and the sound while the user is looking", async () => {
		installNotificationMock("granted");
		setLooking(true);
		const bridge = createWebNotificationsBridge({ soundEnabled: async () => true });
		const sound = vi.fn();
		bridge.onPlaySound(sound);

		await bridge.show({ id: "n1", title: "Agent needs your input", type: "needs_input" });
		setLooking(false);
		await bridge.show({ id: "n2", title: "Agent needs your input", type: "needs_input", watched: true });

		expect(toasts).toHaveLength(0);
		expect(sound).not.toHaveBeenCalled();
	});

	it("plays the sound only for attention types and only when sound is enabled", async () => {
		installNotificationMock("granted");
		setLooking(false);
		let enabled = true;
		const bridge = createWebNotificationsBridge({ soundEnabled: async () => enabled });
		const sound = vi.fn();
		const unsubscribe = bridge.onPlaySound(sound);

		await bridge.show({ id: "n1", title: "PR merged", type: "pr_merged" });
		expect(sound).not.toHaveBeenCalled();

		await bridge.show({ id: "n2", title: "Ready to merge", type: "ready_to_merge" });
		expect(sound).toHaveBeenCalledTimes(1);

		enabled = false;
		await bridge.show({ id: "n3", title: "Agent needs your input", type: "needs_input" });
		expect(sound).toHaveBeenCalledTimes(1);

		enabled = true;
		unsubscribe();
		await bridge.show({ id: "n4", title: "Agent needs your input", type: "needs_input" });
		expect(sound).toHaveBeenCalledTimes(1);
	});

	it("forwards a toast click to onClick listeners and closes the toast", async () => {
		installNotificationMock("granted");
		setLooking(false);
		const focus = vi.spyOn(window, "focus").mockImplementation(() => undefined);
		const bridge = createWebNotificationsBridge({ soundEnabled: async () => false });
		const clicked = vi.fn();
		bridge.onClick(clicked);

		await bridge.show({ id: "n1", title: "Agent needs your input" });
		toasts[0].onclick?.();

		expect(clicked).toHaveBeenCalledWith("n1");
		expect(toasts[0].close).toHaveBeenCalled();
		expect(focus).toHaveBeenCalled();
	});

	it("does nothing when the browser has no Notification API", async () => {
		vi.stubGlobal("Notification", undefined);
		setLooking(false);
		const bridge = createWebNotificationsBridge({ soundEnabled: async () => true });

		await expect(bridge.show({ id: "n1", title: "Agent needs your input" })).resolves.toBeUndefined();
		expect(webNotificationPermission()).toBe("unsupported");
		await expect(requestWebNotificationPermission()).resolves.toBe("unsupported");
	});

	it("prefixes document.title with the unread count and removes it at zero", async () => {
		const bridge = createWebNotificationsBridge({ soundEnabled: async () => true });

		await bridge.setBadge(3);
		expect(document.title).toBe("(3) AO");
		await bridge.setBadge(12);
		expect(document.title).toBe("(12) AO");
		await bridge.setBadge(0);
		expect(document.title).toBe("AO");
	});
});

describe("permission request", () => {
	it("asks the browser only while the permission is still undecided", async () => {
		const api = installNotificationMock("default", "granted");

		await expect(requestWebNotificationPermission()).resolves.toBe("granted");
		await expect(requestWebNotificationPermission()).resolves.toBe("granted");

		expect(api.requestPermission).toHaveBeenCalledTimes(1);
	});
});

describe("badgedTitle", () => {
	it.each([
		["AO", 2, "(2) AO"],
		["(2) AO", 5, "(5) AO"],
		["(2) AO", 0, "AO"],
		["AO", -1, "AO"],
		["AO", Number.NaN, "AO"],
		["AO", 2.7, "(2) AO"],
	])("%s with %s → %s", (title, count, expected) => {
		expect(badgedTitle(title, count)).toBe(expected);
	});
});
