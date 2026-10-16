import type { AoBridge } from "../../preload";
import {
	APP_SHORTCUTS,
	shortcutBindingValidationError,
	type AppShortcutId,
	type KeybindingOverrides,
	type ShortcutBinding,
} from "../../shared/shortcuts";
import { coerceUiSettings } from "../../shared/ui-locale";
import { isMacPlatform } from "./platform";

// In Electron the main process keeps UI settings and keybinding overrides in
// ~/.ao. The web build has no main process, so the browser stub keeps them in
// this browser's localStorage instead. That is per-browser UI preference, not
// daemon app state. Storage can be missing, blocked, full or hold garbage, so
// every read falls back to the defaults and a failed write only loses
// persistence across reloads.

export const UI_SETTINGS_STORAGE_KEY = "ao.uiSettings";
export const KEYBINDINGS_STORAGE_KEY = "ao.keybindings";

function readStoredJson(key: string): unknown {
	try {
		const raw = window.localStorage.getItem(key);
		return raw === null ? null : JSON.parse(raw);
	} catch {
		return null;
	}
}

function writeStoredJson(key: string, value: unknown): void {
	try {
		window.localStorage.setItem(key, JSON.stringify(value));
	} catch {
		// Blocked or full storage: the caller still gets the new value for this page.
	}
}

const customizableIds = new Set<AppShortcutId>(
	APP_SHORTCUTS.filter((shortcut) => shortcut.customizable !== false).map((shortcut) => shortcut.id),
);

// Mirrors coerceKeybindingOverrides in main/keybinding-settings.ts, which the
// renderer cannot import because it pulls in node:fs.
function coerceBinding(raw: unknown, isMac: boolean): ShortcutBinding | null {
	if (!raw || typeof raw !== "object") return null;
	const value = raw as Record<string, unknown>;
	if (typeof value.key !== "string" || value.key.length === 0 || value.key.length > 32) return null;
	const code = typeof value.code === "string" && value.code.length > 0 && value.code.length <= 32 ? value.code : undefined;
	const candidate: ShortcutBinding = {
		key: value.key,
		...(code ? { code } : {}),
		ctrl: value.ctrl === true,
		meta: value.meta === true,
		shift: value.shift === true,
		alt: value.alt === true,
	};
	if (shortcutBindingValidationError(candidate, isMac)) return null;
	return candidate;
}

export function coerceWebKeybindingOverrides(raw: unknown, isMac = isMacPlatform()): KeybindingOverrides {
	if (!raw || typeof raw !== "object") return {};
	const source = raw as Record<string, unknown>;
	const overrides: KeybindingOverrides = {};
	for (const id of customizableIds) {
		const rawBindings = source[id];
		if (!Array.isArray(rawBindings)) continue;
		const bindings = rawBindings
			.slice(0, 2)
			.map((binding) => coerceBinding(binding, isMac))
			.filter((candidate): candidate is ShortcutBinding => candidate !== null);
		// An empty array means "unassigned"; a non-empty one with no valid
		// bindings is dropped so the defaults come back.
		if (rawBindings.length > 0 && bindings.length === 0) continue;
		overrides[id] = bindings;
	}
	return overrides;
}

export const webUiSettings: AoBridge["uiSettings"] = {
	get: async () => coerceUiSettings(readStoredJson(UI_SETTINGS_STORAGE_KEY)),
	set: async (patch) => {
		const current = coerceUiSettings(readStoredJson(UI_SETTINGS_STORAGE_KEY));
		const next = coerceUiSettings({ ...current, ...patch });
		writeStoredJson(UI_SETTINGS_STORAGE_KEY, next);
		return next;
	},
};

// Reset goes through set: the keybindings store persists {} (reset all) or the
// overrides without the reset id (reset one).
export const webKeybindings: Pick<AoBridge["keybindings"], "get" | "set"> = {
	get: async () => coerceWebKeybindingOverrides(readStoredJson(KEYBINDINGS_STORAGE_KEY)),
	set: async (overrides) => {
		const next = coerceWebKeybindingOverrides(overrides);
		writeStoredJson(KEYBINDINGS_STORAGE_KEY, next);
		return next;
	},
};
