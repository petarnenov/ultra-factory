import { useEffect, useMemo, useRef } from "react";
import { useQuery } from "@tanstack/react-query";
import type { UpdateState, UpdateStatus } from "../../../main/update-settings";
import { useRequestUpdateInstall } from "../../hooks/useRequestUpdateInstall";
import { requestUpdateDownload, useUpdateStatus } from "../../hooks/useUpdateStatus";
import { aoBridge } from "../../lib/bridge";
import { registerTool, ToolError, type ToolDescriptor } from "../model-context";

const DEFAULT_WAIT_TIMEOUT_MS = 5000;
const UPDATES_SECTION = "updates";

export type SettingsNavItem = { id: string; label: string; active: boolean; onSelect: () => void };

export type UpdatesRead = { state: UpdateState; version?: string; percent?: number; appVersion: string | null };

export type SettingsRead = {
	open: boolean;
	activeSection: string | null;
	sections: { id: string; label: string }[];
	updates: UpdatesRead | null;
};

// The update actions mirror the Updates section buttons, guards included.
export type UpdatesContext = {
	status: UpdateStatus;
	appVersion: string | null;
	download: () => void;
	install: () => void;
	check: () => Promise<void>;
};

// What global Settings renders; `updates` is null when that section is hidden.
export type SettingsContext = {
	navItems: SettingsNavItem[];
	updates: UpdatesContext | null;
};

// `current` is undefined once global Settings closes. Waiters re-read through
// it on every notification, never from a captured snapshot.
export type SettingsAccess = {
	current: () => SettingsContext | undefined;
	subscribe: (listener: () => void) => () => void;
};

export type SelectSectionArgs = { id: string; timeoutMs?: number };
export type WaitForUpdateStateArgs = { state: string; timeoutMs?: number };

export function readSettings(context: SettingsContext): SettingsRead {
	return {
		open: true,
		activeSection: context.navItems.find((item) => item.active)?.id ?? null,
		sections: context.navItems.map(({ id, label }) => ({ id, label })),
		updates: context.updates && readUpdates(context.updates),
	};
}

export function settingsTools(access: SettingsAccess, sectionIds: string[]): ToolDescriptor[] {
	const tools: ToolDescriptor[] = [
		{
			name: "settings.read",
			description: "Return settings sections, the active one, and update state.",
			inputSchema: { type: "object" },
			execute: () => readSettings(requireContext(access)),
		},
		{
			name: "settings.select_section",
			description: "Activate a settings section by id.",
			inputSchema: {
				type: "object",
				required: ["id"],
				properties: { id: { type: "string", enum: sectionIds }, timeoutMs: { type: "number" } },
			},
			execute: (args) => selectSection(access, args as SelectSectionArgs),
		},
	];
	if (!sectionIds.includes(UPDATES_SECTION)) return tools;
	return [...tools, ...updateTools(access)];
}

function updateTools(access: SettingsAccess): ToolDescriptor[] {
	return [
		{
			name: "settings.update.download",
			description: "Start downloading the available update.",
			inputSchema: { type: "object" },
			execute: () => {
				const updates = requireUpdates(access);
				if (updates.status.state !== "available") throw notAllowed("download", updates.status);
				updates.download();
				return { requested: true };
			},
		},
		{
			name: "settings.update.install",
			description: "Install the downloaded update.",
			inputSchema: { type: "object" },
			execute: () => {
				const updates = requireUpdates(access);
				if (!canInstall(updates.status)) throw notAllowed("install", updates.status);
				updates.install();
				return { requested: true };
			},
		},
		{
			name: "settings.update.check",
			description: "Check for updates now.",
			inputSchema: { type: "object" },
			execute: async () => {
				const updates = requireUpdates(access);
				if (isDownloading(updates.status)) throw notAllowed("check", updates.status);
				await updates.check();
				return { requested: true };
			},
		},
		{
			name: "settings.wait_for_update_state",
			description: "Resolve when update state equals the given value, or fail after timeoutMs.",
			inputSchema: {
				type: "object",
				required: ["state"],
				properties: { state: { type: "string" }, timeoutMs: { type: "number" } },
			},
			execute: (args) => waitForUpdateState(access, args as WaitForUpdateStateArgs),
		},
	];
}

// Registers the settings tools while global Settings is open: the caller
// mounts this only for the open global layer, so unmounting unregisters them.
export function useSettingsTools(navItems: SettingsNavItem[]): void {
	const status = useUpdateStatus();
	const install = useRequestUpdateInstall();
	const appVersion = useQuery({ queryKey: ["app-version"], queryFn: () => aoBridge.app.getVersion() }).data ?? null;
	const checkPending = useRef(false);
	const showsUpdates = navItems.some((item) => item.id === UPDATES_SECTION);

	const check = async () => {
		if (checkPending.current) throw new ToolError("precondition", "an update check is already running");
		checkPending.current = true;
		try {
			await aoBridge.updates.check({ requestId: `webmcp-update-${Date.now()}` });
		} finally {
			checkPending.current = false;
		}
	};
	const context: SettingsContext = {
		navItems,
		updates: showsUpdates
			? { status, appVersion, download: () => void requestUpdateDownload(), install, check }
			: null,
	};

	const latest = useRef<SettingsContext | undefined>(context);
	const listeners = useRef(new Set<() => void>());
	latest.current = context;

	// Runs after every commit, so waiters see what the DOM shows.
	useEffect(() => {
		for (const listener of listeners.current) listener();
	});
	useEffect(() => {
		latest.current = context;
		return () => {
			// Settings closed: waiters observe it and reject.
			latest.current = undefined;
			for (const listener of listeners.current) listener();
		};
	}, []);

	const access = useMemo<SettingsAccess>(
		() => ({
			current: () => latest.current,
			subscribe: (listener) => {
				listeners.current.add(listener);
				return () => {
					listeners.current.delete(listener);
				};
			},
		}),
		[],
	);
	// Section visibility settles after open (the cloud offering loads late),
	// so the select_section enum re-registers when the id list changes.
	const sectionsKey = navItems.map((item) => item.id).join(",");
	const tools = useMemo(() => settingsTools(access, sectionsKey.split(",")), [access, sectionsKey]);
	useEffect(() => {
		const unregisters = tools.map((tool) => registerTool(tool));
		return () => unregisters.forEach((unregister) => unregister());
	}, [tools]);
}

function readUpdates(updates: UpdatesContext): UpdatesRead {
	const { state, version, percent } = updates.status;
	return { state, version, percent, appVersion: updates.appVersion };
}

function isDownloading(status: UpdateStatus): boolean {
	return status.state === "downloading" || status.state === "preparing";
}

// Same availability as the Restart & install button.
function canInstall(status: UpdateStatus): boolean {
	return !isDownloading(status) && (status.state === "downloaded" || (status.staged !== undefined && status.staged.ready !== false));
}

function notAllowed(action: string, status: UpdateStatus): ToolError {
	return new ToolError("precondition", `cannot ${action} while the update state is ${status.state}`);
}

function requireContext(access: SettingsAccess): SettingsContext {
	const context = access.current();
	if (!context) throw new ToolError("precondition", "global settings is not open");
	return context;
}

function requireUpdates(access: SettingsAccess): UpdatesContext {
	const updates = requireContext(access).updates;
	if (!updates) throw new ToolError("precondition", "the updates section is not available");
	return updates;
}

function selectSection(access: SettingsAccess, { id, timeoutMs = DEFAULT_WAIT_TIMEOUT_MS }: SelectSectionArgs) {
	const item = requireContext(access).navItems.find((candidate) => candidate.id === id);
	if (!item) throw new ToolError("precondition", `${id} is not a visible settings section`);
	item.onSelect();
	// The selection is React state: resolve once the commit shows it active.
	return waitFor(access, timeoutMs, `${id} did not become active`, (context) =>
		context.navItems.some((candidate) => candidate.id === id && candidate.active) ? { activeSection: id } : undefined,
	);
}

function waitForUpdateState(access: SettingsAccess, { state, timeoutMs = DEFAULT_WAIT_TIMEOUT_MS }: WaitForUpdateStateArgs) {
	return waitFor(access, timeoutMs, `update state is not ${state}`, (context) => {
		const updates = context.updates && readUpdates(context.updates);
		return updates?.state === state ? updates : undefined;
	});
}

function waitFor<R>(
	access: SettingsAccess,
	timeoutMs: number,
	failure: string,
	match: (context: SettingsContext) => R | undefined,
): Promise<R> {
	return new Promise<R>((resolve, reject) => {
		let unsubscribe = () => {};
		const settle = () => {
			clearTimeout(timer);
			unsubscribe();
		};
		const timer = setTimeout(() => {
			settle();
			reject(new ToolError("precondition", `${failure} after ${timeoutMs}ms`));
		}, timeoutMs);
		const check = () => {
			const context = access.current();
			if (!context) {
				settle();
				reject(new ToolError("precondition", "global settings closed"));
				return;
			}
			const result = match(context);
			if (result === undefined) return;
			settle();
			resolve(result);
		};
		unsubscribe = access.subscribe(check);
		check();
	});
}
