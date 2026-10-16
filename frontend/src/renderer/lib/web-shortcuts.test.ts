import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { KeybindingOverrides } from "../../shared/shortcuts";
import { installWebShortcuts, setWebShortcutRecording, webShortcutListeners } from "./web-shortcuts";

function press(init: KeyboardEventInit, target: EventTarget = window): KeyboardEvent {
	const event = new KeyboardEvent("keydown", { bubbles: true, cancelable: true, ...init });
	target.dispatchEvent(event);
	return event;
}

describe("web shortcuts", () => {
	let overrides: KeybindingOverrides;
	const disposers: Array<() => void> = [];

	function install(isMac: boolean, web = true) {
		disposers.push(installWebShortcuts({ isMac, getOverrides: () => overrides, web }));
	}

	function listen<T extends (listener: never) => () => void>(subscribe: T): ReturnType<typeof vi.fn> {
		const listener = vi.fn();
		disposers.push(subscribe(listener as never));
		return listener;
	}

	beforeEach(() => {
		overrides = {};
	});

	afterEach(() => {
		while (disposers.length > 0) disposers.pop()?.();
		webShortcutListeners.setCloseShellTerminalShortcutEnabled(false);
		setWebShortcutRecording(false);
		document.body.innerHTML = "";
	});

	it("fires the dedicated listeners for the web Alt chords", () => {
		install(false);
		const newSession = listen(webShortcutListeners.onNewSessionShortcut);
		const newTerminal = listen(webShortcutListeners.onNewShellTerminalShortcut);
		const previousSession = listen(webShortcutListeners.onPreviousSessionShortcut);
		const nextSession = listen(webShortcutListeners.onNextSessionShortcut);
		const previousTab = listen(webShortcutListeners.onPreviousTabShortcut);
		const nextTab = listen(webShortcutListeners.onNextTabShortcut);

		const event = press({ key: "N", code: "KeyN", altKey: true, shiftKey: true });
		press({ key: "T", code: "KeyT", altKey: true, shiftKey: true });
		press({ key: "ArrowUp", altKey: true });
		press({ key: "ArrowDown", altKey: true });
		press({ key: "{", code: "BracketLeft", altKey: true, shiftKey: true });
		press({ key: "}", code: "BracketRight", altKey: true, shiftKey: true });

		expect(event.defaultPrevented).toBe(true);
		for (const listener of [newSession, newTerminal, previousSession, nextSession, previousTab, nextTab]) {
			expect(listener).toHaveBeenCalledTimes(1);
		}
	});

	it("matches macOS Option chords by physical key", () => {
		install(true);
		const newSession = listen(webShortcutListeners.onNewSessionShortcut);
		const shortcut = listen(webShortcutListeners.onShortcut);

		press({ key: "˜", code: "KeyN", altKey: true, shiftKey: true });
		press({ key: "™", code: "Digit2", altKey: true });

		expect(newSession).toHaveBeenCalledTimes(1);
		expect(shortcut).toHaveBeenCalledWith({ id: "jump-to-session", index: 1 });
	});

	it("leaves the browser-reserved desktop chords to the browser", () => {
		install(true);
		const newSession = listen(webShortcutListeners.onNewSessionShortcut);
		const newTerminal = listen(webShortcutListeners.onNewShellTerminalShortcut);

		const event = press({ key: "n", code: "KeyN", metaKey: true });
		press({ key: "t", code: "KeyT", metaKey: true });

		expect(event.defaultPrevented).toBe(false);
		expect(newSession).not.toHaveBeenCalled();
		expect(newTerminal).not.toHaveBeenCalled();
	});

	it("keeps the desktop chords that browsers let a page handle", () => {
		install(true);
		const help = listen(webShortcutListeners.onKeyboardShortcutsHelp);
		const settings = listen(webShortcutListeners.onOpenSettingsShortcut);
		const shortcut = listen(webShortcutListeners.onShortcut);

		press({ key: "/", code: "Slash", metaKey: true });
		press({ key: ",", code: "Comma", metaKey: true });
		press({ key: "k", code: "KeyK", metaKey: true });

		expect(help).toHaveBeenCalledTimes(1);
		expect(settings).toHaveBeenCalledWith();
		expect(shortcut).toHaveBeenCalledWith({ id: "command-palette" });
	});

	it("follows the current keybinding overrides", () => {
		overrides = { "new-session": [{ key: "y", ctrl: true, meta: false, shift: true, alt: false }] };
		install(false);
		const newSession = listen(webShortcutListeners.onNewSessionShortcut);

		press({ key: "N", code: "KeyN", altKey: true, shiftKey: true });
		expect(newSession).not.toHaveBeenCalled();
		press({ key: "Y", code: "KeyY", ctrlKey: true, shiftKey: true });
		expect(newSession).toHaveBeenCalledTimes(1);
	});

	it("closes a tab only while a closable tab is active", () => {
		install(false);
		const close = listen(webShortcutListeners.onCloseShellTerminalShortcut);

		const ignored = press({ key: "W", code: "KeyW", altKey: true, shiftKey: true });
		webShortcutListeners.setCloseShellTerminalShortcutEnabled(true);
		press({ key: "W", code: "KeyW", altKey: true, shiftKey: true });

		expect(ignored.defaultPrevented).toBe(false);
		expect(close).toHaveBeenCalledTimes(1);
	});

	it("consumes key repeats without firing again", () => {
		install(false);
		const newTerminal = listen(webShortcutListeners.onNewShellTerminalShortcut);

		const repeat = press({ key: "T", code: "KeyT", altKey: true, shiftKey: true, repeat: true });

		expect(repeat.defaultPrevented).toBe(true);
		expect(newTerminal).not.toHaveBeenCalled();
	});

	it("lets a focused terminal keep its readline chords on Windows/Linux", () => {
		install(false, false);
		const shortcut = listen(webShortcutListeners.onShortcut);
		const terminal = document.createElement("div");
		terminal.className = "xterm";
		const textarea = document.createElement("textarea");
		terminal.append(textarea);
		document.body.append(terminal);

		press({ key: "k", code: "KeyK", ctrlKey: true }, textarea);
		expect(shortcut).not.toHaveBeenCalled();
		press({ key: "k", code: "KeyK", ctrlKey: true });
		expect(shortcut).toHaveBeenCalledWith({ id: "command-palette" });
	});

	it("stands down while a binding is being recorded", () => {
		install(false);
		const newSession = listen(webShortcutListeners.onNewSessionShortcut);

		setWebShortcutRecording(true);
		const event = press({ key: "N", code: "KeyN", altKey: true, shiftKey: true });

		expect(event.defaultPrevented).toBe(false);
		expect(newSession).not.toHaveBeenCalled();
	});

	it("stops listening once disposed", () => {
		install(false);
		const newSession = listen(webShortcutListeners.onNewSessionShortcut);
		disposers.shift()?.();

		press({ key: "N", code: "KeyN", altKey: true, shiftKey: true });
		expect(newSession).not.toHaveBeenCalled();
	});
});
