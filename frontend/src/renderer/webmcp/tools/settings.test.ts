import { describe, expect, it, vi } from "vitest";
import type { UpdateStatus } from "../../../main/update-settings";
import { visibleGlobalSettings } from "../../components/settings/settingsCatalog";
import { ToolError, type ToolDescriptor } from "../model-context";
import {
	readSettings,
	settingsTools,
	type SettingsAccess,
	type SettingsContext,
	type SettingsNavItem,
	type UpdatesContext,
} from "./settings";

const gates = { cloudEnabled: false, developerMode: false, diagnostics: false, hasCoder: false, selfServeBilling: false };

// A settings layer whose selection commits like React state: the next notify.
function fakeSettings(ids: string[], updateStatus?: UpdateStatus) {
	let active = ids[0];
	let open = true;
	const listeners = new Set<() => void>();
	const notify = () => listeners.forEach((listener) => listener());
	const onSelect = vi.fn((id: string) => {
		active = id;
		queueMicrotask(notify);
	});
	const updates: UpdatesContext | null = updateStatus
		? { status: updateStatus, appVersion: "1.0.0", download: vi.fn(), install: vi.fn(), check: vi.fn(async () => undefined) }
		: null;
	const navItems = (): SettingsNavItem[] =>
		ids.map((id) => ({ id, label: id.toUpperCase(), active: id === active, onSelect: () => onSelect(id) }));
	const access: SettingsAccess = {
		current: (): SettingsContext | undefined => (open ? { navItems: navItems(), updates } : undefined),
		subscribe: (listener) => {
			listeners.add(listener);
			return () => listeners.delete(listener);
		},
	};
	const close = () => {
		open = false;
		notify();
	};
	const tools = settingsTools(access, ids);
	const tool = (name: string): ToolDescriptor => {
		const found = tools.find((candidate) => candidate.name === name);
		if (!found) throw new Error(`missing ${name}`);
		return found;
	};
	return { access, onSelect, updates, close, tools, tool };
}

describe("settings tools", () => {
	it("reads exactly the visible catalog sections", () => {
		const ids = visibleGlobalSettings(gates).map((section) => section.id);
		const settings = fakeSettings(ids);

		const read = readSettings(settings.access.current()!);

		expect(read.sections.map((section) => section.id)).toEqual(ids);
		expect(read.activeSection).toBe(ids[0]);
	});

	it("select_section calls the matching onSelect and resolves once it is active", async () => {
		const settings = fakeSettings(["general", "updates", "help"], { state: "idle" });

		await expect(settings.tool("settings.select_section").execute({ id: "updates" })).resolves.toEqual({
			activeSection: "updates",
		});

		expect(settings.onSelect).toHaveBeenCalledExactlyOnceWith("updates");
		expect(readSettings(settings.access.current()!).activeSection).toBe("updates");
	});

	it("select_section rejects an unknown section with precondition", () => {
		const settings = fakeSettings(["general", "help"]);

		expect(() => settings.tool("settings.select_section").execute({ id: "updates" })).toThrow(
			expect.objectContaining({ code: "precondition" }),
		);
		expect(settings.onSelect).not.toHaveBeenCalled();
	});

	it("omits the update tools when the updates section is hidden", () => {
		const settings = fakeSettings(["general", "help"]);

		expect(settings.tools.map((tool) => tool.name)).toEqual(["settings.read", "settings.select_section"]);
	});

	it("downloads only while an update is available", () => {
		const available = fakeSettings(["updates"], { state: "available", version: "2.0.0" });
		available.tool("settings.update.download").execute({});
		expect(available.updates!.download).toHaveBeenCalledOnce();

		const idle = fakeSettings(["updates"], { state: "idle" });
		expect(() => idle.tool("settings.update.download").execute({})).toThrow(ToolError);
		expect(idle.updates!.download).not.toHaveBeenCalled();
	});

	it("installs only what the Restart & install button offers", () => {
		const downloaded = fakeSettings(["updates"], { state: "downloaded", version: "2.0.0" });
		downloaded.tool("settings.update.install").execute({});
		expect(downloaded.updates!.install).toHaveBeenCalledOnce();

		const preparing = fakeSettings(["updates"], {
			state: "preparing",
			staged: { version: "2.0.0", stagedAt: 1, escalated: false, ready: false },
		});
		expect(() => preparing.tool("settings.update.install").execute({})).toThrow(ToolError);
	});

	it("refuses a check while a download is running", async () => {
		const downloading = fakeSettings(["updates"], { state: "downloading" });

		await expect(downloading.tool("settings.update.check").execute({})).rejects.toMatchObject({ code: "precondition" });
		expect(downloading.updates!.check).not.toHaveBeenCalled();
	});

	it("wait_for_update_state resolves on a matching state and rejects when settings closes", async () => {
		const settings = fakeSettings(["updates"], { state: "downloading", version: "2.0.0", percent: 5 });
		await expect(settings.tool("settings.wait_for_update_state").execute({ state: "downloading" })).resolves.toEqual({
			state: "downloading",
			version: "2.0.0",
			percent: 5,
			appVersion: "1.0.0",
		});

		const waiting = settings.tool("settings.wait_for_update_state").execute({ state: "downloaded" });
		settings.close();
		await expect(waiting).rejects.toMatchObject({ code: "precondition" });
	});
});
