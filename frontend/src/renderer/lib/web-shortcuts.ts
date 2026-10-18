import type { AoBridge } from "../../preload";
import {
	APP_SHORTCUTS,
	matchesAppShortcut,
	shortcutYieldsToTerminal,
	type AppShortcutEvent,
	type AppShortcutId,
	type KeybindingOverrides,
	type ShortcutChord,
} from "../../shared/shortcuts";

// In Electron the main process catches application shortcuts and sends them
// over IPC; here the aoBridge stub's listeners fire from a global keydown.

type Emitter<Args extends unknown[]> = {
	on: (listener: (...args: Args) => void) => () => void;
	emit: (...args: Args) => void;
};

function emitter<Args extends unknown[]>(): Emitter<Args> {
	const listeners = new Set<(...args: Args) => void>();
	return {
		on: (listener) => {
			listeners.add(listener);
			return () => {
				listeners.delete(listener);
			};
		},
		emit: (...args) => {
			for (const listener of [...listeners]) listener(...args);
		},
	};
}

const newSession = emitter<[]>();
const keyboardShortcutsHelp = emitter<[]>();
const newShellTerminal = emitter<[]>();
const closeShellTerminal = emitter<[]>();
const openSettings = emitter<[section?: string]>();
const previousSession = emitter<[]>();
const nextSession = emitter<[]>();
const previousTab = emitter<[]>();
const nextTab = emitter<[]>();
const appShortcut = emitter<[event: AppShortcutEvent]>();

// Same split as legacyShortcutChannels in main/app-shortcuts.ts: these commands
// have their own listener, every other one goes through onShortcut.
const legacyShortcuts: Partial<Record<AppShortcutId, () => void>> = {
	"new-session": () => newSession.emit(),
	"new-shell-terminal": () => newShellTerminal.emit(),
	"close-shell-terminal": () => closeShellTerminal.emit(),
	"keyboard-shortcuts": () => keyboardShortcutsHelp.emit(),
	"open-settings": () => openSettings.emit(),
	"previous-session": () => previousSession.emit(),
	"next-session": () => nextSession.emit(),
	"previous-tab": () => previousTab.emit(),
	"next-tab": () => nextTab.emit(),
};

// Main opens the Browser DevTools; in a browser there is nothing to toggle.
const desktopOnlyShortcuts: ReadonlySet<AppShortcutId> = new Set(["toggle-browser-devtools"]);

const TERMINAL_SELECTOR = ".xterm, .terminal-surface, [data-testid='session-terminal']";

let closeShellTerminalEnabled = false;
let recording = false;

export type WebShortcutListeners = Pick<
	AoBridge["app"],
	| "onNewSessionShortcut"
	| "onKeyboardShortcutsHelp"
	| "onNewShellTerminalShortcut"
	| "onCloseShellTerminalShortcut"
	| "setCloseShellTerminalShortcutEnabled"
	| "onOpenSettingsShortcut"
	| "onPreviousSessionShortcut"
	| "onNextSessionShortcut"
	| "onPreviousTabShortcut"
	| "onNextTabShortcut"
	| "onShortcut"
>;

export const webShortcutListeners: WebShortcutListeners = {
	onNewSessionShortcut: newSession.on,
	onKeyboardShortcutsHelp: keyboardShortcutsHelp.on,
	onNewShellTerminalShortcut: newShellTerminal.on,
	onCloseShellTerminalShortcut: closeShellTerminal.on,
	setCloseShellTerminalShortcutEnabled: (enabled) => {
		closeShellTerminalEnabled = enabled;
	},
	onOpenSettingsShortcut: openSettings.on,
	onPreviousSessionShortcut: previousSession.on,
	onNextSessionShortcut: nextSession.on,
	onPreviousTabShortcut: previousTab.on,
	onNextTabShortcut: nextTab.on,
	onShortcut: appShortcut.on,
};

/** While Settings records a new binding, key presses go to the recorder. */
export function setWebShortcutRecording(active: boolean): void {
	recording = active;
}

// ⌥1 on macOS types "¡" while "1-9" only matches a digit key, so the digit
// comes from the physical key.
function keyboardChord(event: KeyboardEvent): ShortcutChord {
	const digit = /^Digit([1-9])$/.exec(event.code ?? "");
	return {
		key: digit && !/^[1-9]$/.test(event.key) ? digit[1] : event.key,
		code: event.code,
		ctrl: event.ctrlKey,
		meta: event.metaKey,
		shift: event.shiftKey,
		alt: event.altKey,
	};
}

function matchingShortcut(
	chord: ShortcutChord,
	isMac: boolean,
	overrides: KeybindingOverrides,
	web: boolean | undefined,
): AppShortcutId | null {
	for (const { id } of APP_SHORTCUTS) {
		if (desktopOnlyShortcuts.has(id)) continue;
		// web === undefined keeps the build default.
		if (matchesAppShortcut(id, chord, isMac, overrides, web)) return id;
	}
	return null;
}

export type WebShortcutOptions = {
	isMac: boolean;
	getOverrides: () => KeybindingOverrides;
	target?: Window;
	/** Tests only: forces web or desktop defaults regardless of the build. */
	web?: boolean;
};

/** Listens for keydown in the window capture phase, ahead of xterm and text
 * fields, and fires the listeners of the matching command. */
export function installWebShortcuts({ isMac, getOverrides, target = window, web }: WebShortcutOptions): () => void {
	const onKeyDown = (event: KeyboardEvent) => {
		if (recording || event.isComposing || event.defaultPrevented) return;
		const chord = keyboardChord(event);
		const id = matchingShortcut(chord, isMac, getOverrides(), web);
		if (!id) return;
		const element = event.target instanceof Element ? event.target : null;
		if (!isMac && shortcutYieldsToTerminal(id, chord) && element?.closest(TERMINAL_SELECTOR)) return;
		// With no closable tab the chord stays with the page, as in main.
		if (id === "close-shell-terminal" && !closeShellTerminalEnabled) return;
		// preventDefault also tells useAppShortcut the command is already handled.
		event.preventDefault();
		// A held key does not repeat the command.
		if (event.repeat) return;
		const legacy = legacyShortcuts[id];
		if (legacy) {
			legacy();
			return;
		}
		appShortcut.emit(id === "jump-to-session" ? { id, index: Number(chord.key) - 1 } : { id });
	};
	target.addEventListener("keydown", onKeyDown, { capture: true });
	return () => target.removeEventListener("keydown", onKeyDown, { capture: true });
}
