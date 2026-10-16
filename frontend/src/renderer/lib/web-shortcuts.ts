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

// В Electron main процесът хваща application shortcut-ите в before-input-event
// и ги праща към renderer-а по IPC (main/app-shortcuts.ts). Web build-ът няма
// main процес, затова този модул поема същата роля в страницата: държи
// listener-ите, които stub-ът на aoBridge.app.on*Shortcut регистрира, и ги
// задейства от глобален keydown handler. Handler-ът се инсталира само при
// isWebBuild, така че в Electron комбинациите не се дублират.

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

// Същото разделение като legacyShortcutChannels в main/app-shortcuts.ts: тези
// команди имат собствен listener, всички останали минават през onShortcut.
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

// Browser DevTools се отварят от main; в браузъра няма какво да се задейства.
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

/** Докато Settings записва нова комбинация, натиснатите клавиши отиват при нея. */
export function setWebShortcutRecording(active: boolean): void {
	recording = active;
}

// ⌥1 на macOS дава "¡", а "1-9" съвпада само с цифра в key, затова цифрата се
// взима от физическия клавиш.
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
		// web === undefined оставя default-а на build-а.
		if (matchesAppShortcut(id, chord, isMac, overrides, web)) return id;
	}
	return null;
}

export type WebShortcutOptions = {
	isMac: boolean;
	getOverrides: () => KeybindingOverrides;
	target?: Window;
	/** Само за тестове: налага web или desktop defaults независимо от build-а. */
	web?: boolean;
};

/**
 * Слуша keydown в capture фазата на window, преди xterm и полетата за текст, и
 * задейства listener-ите на съответната команда. Връща функция за премахване.
 */
export function installWebShortcuts({ isMac, getOverrides, target = window, web }: WebShortcutOptions): () => void {
	const onKeyDown = (event: KeyboardEvent) => {
		if (recording || event.isComposing || event.defaultPrevented) return;
		const chord = keyboardChord(event);
		const id = matchingShortcut(chord, isMac, getOverrides(), web);
		if (!id) return;
		const element = event.target instanceof Element ? event.target : null;
		if (!isMac && shortcutYieldsToTerminal(id, chord) && element?.closest(TERMINAL_SELECTOR)) return;
		// Без отворен таб комбинацията остава за страницата, както в main.
		if (id === "close-shell-terminal" && !closeShellTerminalEnabled) return;
		// preventDefault казва и на useAppShortcut, че командата вече е поета.
		event.preventDefault();
		// Задържан клавиш не повтаря командата.
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
