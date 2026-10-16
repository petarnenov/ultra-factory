import { afterEach, describe, expect, it, vi } from "vitest";
import { DEFAULT_UI_SETTINGS } from "../../shared/ui-locale";
import {
	KEYBINDINGS_STORAGE_KEY,
	UI_SETTINGS_STORAGE_KEY,
	coerceWebKeybindingOverrides,
	webKeybindings,
	webUiSettings,
} from "./web-settings";

const ctrlShiftK = { key: "K", ctrl: true, meta: false, shift: true, alt: false };

afterEach(() => {
	vi.restoreAllMocks();
	window.localStorage.clear();
});

describe("web ui settings", () => {
	it("returns the defaults when nothing is stored", async () => {
		await expect(webUiSettings.get()).resolves.toEqual(DEFAULT_UI_SETTINGS);
	});

	it("merges a patch into the stored settings and survives a re-read", async () => {
		await webUiSettings.set({ locale: "de" });
		const next = await webUiSettings.set({ soundNotificationsEnabled: false });

		expect(next).toEqual({ ...DEFAULT_UI_SETTINGS, locale: "de", soundNotificationsEnabled: false });
		await expect(webUiSettings.get()).resolves.toEqual(next);
		expect(JSON.parse(window.localStorage.getItem(UI_SETTINGS_STORAGE_KEY) ?? "null")).toEqual(next);
	});

	it("falls back to the defaults for corrupt or invalid stored data", async () => {
		window.localStorage.setItem(UI_SETTINGS_STORAGE_KEY, "{not json");
		await expect(webUiSettings.get()).resolves.toEqual(DEFAULT_UI_SETTINGS);

		window.localStorage.setItem(UI_SETTINGS_STORAGE_KEY, JSON.stringify({ locale: "xx", soundNotificationsEnabled: "no" }));
		await expect(webUiSettings.get()).resolves.toEqual(DEFAULT_UI_SETTINGS);
	});

	it("falls back to the defaults when storage access throws", async () => {
		vi.spyOn(Storage.prototype, "getItem").mockImplementation(() => {
			throw new DOMException("blocked", "SecurityError");
		});
		vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => {
			throw new DOMException("full", "QuotaExceededError");
		});

		await expect(webUiSettings.get()).resolves.toEqual(DEFAULT_UI_SETTINGS);
		await expect(webUiSettings.set({ locale: "fr" })).resolves.toEqual({ ...DEFAULT_UI_SETTINGS, locale: "fr" });
	});
});

describe("web keybindings", () => {
	it("returns no overrides when nothing is stored", async () => {
		await expect(webKeybindings.get()).resolves.toEqual({});
	});

	it("persists overrides and returns them after a re-read", async () => {
		const saved = await webKeybindings.set({ "open-settings": [ctrlShiftK] });

		expect(saved).toEqual({ "open-settings": [ctrlShiftK] });
		await expect(webKeybindings.get()).resolves.toEqual(saved);
	});

	it("resets through set: removing one id and persisting {}", async () => {
		await webKeybindings.set({ "open-settings": [ctrlShiftK], "new-session": [] });

		await webKeybindings.set({ "new-session": [] });
		await expect(webKeybindings.get()).resolves.toEqual({ "new-session": [] });

		await webKeybindings.set({});
		await expect(webKeybindings.get()).resolves.toEqual({});
	});

	it("drops unknown ids and invalid bindings from stored data", async () => {
		window.localStorage.setItem(
			KEYBINDINGS_STORAGE_KEY,
			JSON.stringify({ "open-settings": [{ key: "" }], "not-a-shortcut": [ctrlShiftK] }),
		);
		await expect(webKeybindings.get()).resolves.toEqual({});

		window.localStorage.setItem(KEYBINDINGS_STORAGE_KEY, "[broken");
		await expect(webKeybindings.get()).resolves.toEqual({});
	});

	it("returns no overrides when storage access throws", async () => {
		vi.spyOn(Storage.prototype, "getItem").mockImplementation(() => {
			throw new DOMException("blocked", "SecurityError");
		});

		await expect(webKeybindings.get()).resolves.toEqual({});
	});

	it("coerces bindings the same way as the desktop store", () => {
		expect(coerceWebKeybindingOverrides({ "open-settings": [{ ...ctrlShiftK, extra: 1 }, ctrlShiftK, ctrlShiftK] }, false)).toEqual({
			"open-settings": [ctrlShiftK, ctrlShiftK],
		});
	});
});
