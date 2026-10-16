import { renderHook } from "@testing-library/react";
import type { QueryClient } from "@tanstack/react-query";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { DaemonStatus } from "../../shared/daemon-status";

// aoBridge picks window.ao or the browser stub when the module loads, so each
// test removes the setup's fake preload and imports a fresh copy.

vi.mock("./event-transport", () => ({
	createEventTransport: () => ({ connect: () => () => undefined }),
}));

vi.mock("../hooks/useAgentReadinessQuery", () => ({
	agentReadinessQueryKey: ["agent-readiness"],
	ensureAgentReadiness: vi.fn(async () => ({ agents: [] })),
	cacheAgentReadiness: vi.fn(),
}));

const fetchMock = vi.fn<typeof fetch>();
let savedBridge: typeof window.ao;

async function loadBridge() {
	vi.resetModules();
	return (await import("./bridge")).aoBridge;
}

function readyResponse(): Response {
	return new Response(JSON.stringify({ status: "ready", service: "ao" }), {
		status: 200,
		headers: { "Content-Type": "application/json" },
	});
}

function notReadyResponse(): Response {
	return new Response("unavailable", { status: 503 });
}

beforeEach(() => {
	savedBridge = window.ao;
	delete (window as { ao?: unknown }).ao;
	fetchMock.mockReset();
	vi.stubGlobal("fetch", fetchMock);
});

afterEach(() => {
	window.ao = savedBridge;
	vi.useRealTimers();
	vi.unstubAllGlobals();
	vi.unstubAllEnvs();
});

describe("web daemon bridge", () => {
	it("reports ready with the page's port when /readyz answers 200", async () => {
		fetchMock.mockImplementation(async () => readyResponse());
		const aoBridge = await loadBridge();

		const status = await aoBridge.daemon.getStatus();

		expect(fetchMock).toHaveBeenCalledWith("/readyz", { cache: "no-store" });
		expect(status).toEqual({ state: "ready", port: Number(window.location.port) || 80 });
	});

	it("probes the explicit API base URL and reports its effective port", async () => {
		vi.stubEnv("VITE_AO_API_BASE_URL", "https://hub.example.test/");
		fetchMock.mockImplementation(async () => readyResponse());
		const aoBridge = await loadBridge();

		const status = await aoBridge.daemon.getStatus();

		expect(fetchMock).toHaveBeenCalledWith("https://hub.example.test/readyz", { cache: "no-store" });
		expect(status).toEqual({ state: "ready", port: 443 });
	});

	it("reports an error when the daemon is unreachable", async () => {
		fetchMock.mockRejectedValue(new TypeError("Failed to fetch"));
		const aoBridge = await loadBridge();

		await expect(aoBridge.daemon.getStatus()).resolves.toEqual({
			state: "error",
			message: "Daemon readiness check failed: Failed to fetch",
		});
	});

	it("reports an error for a non-200 readiness answer", async () => {
		fetchMock.mockImplementation(async () => notReadyResponse());
		const aoBridge = await loadBridge();

		await expect(aoBridge.daemon.getStatus()).resolves.toEqual({
			state: "error",
			message: "Daemon readiness check returned HTTP 503.",
		});
	});

	it("does not mistake a static host's index.html for a ready daemon", async () => {
		fetchMock.mockResolvedValue(new Response("<!doctype html><html></html>", { status: 200 }));
		const aoBridge = await loadBridge();

		await expect(aoBridge.daemon.getStatus()).resolves.toMatchObject({ state: "error" });
	});

	it("marks start, stop, and restart unsupported without controlling the daemon", async () => {
		fetchMock.mockImplementation(async () => readyResponse());
		const aoBridge = await loadBridge();

		for (const control of [aoBridge.daemon.start, aoBridge.daemon.stop, aoBridge.daemon.restart]) {
			await expect(control()).resolves.toMatchObject({ state: "ready", supported: false });
		}
		expect(fetchMock.mock.calls.every(([url]) => url === "/readyz")).toBe(true);
	});

	it("polls every 5 s until ready, then every 30 s", async () => {
		vi.useFakeTimers();
		fetchMock.mockResolvedValueOnce(notReadyResponse()).mockImplementation(async () => readyResponse());
		const aoBridge = await loadBridge();
		const statuses: DaemonStatus[] = [];

		const stop = aoBridge.daemon.onStatus((status) => statuses.push(status));
		await vi.advanceTimersByTimeAsync(4_999);
		expect(fetchMock).not.toHaveBeenCalled();
		await vi.advanceTimersByTimeAsync(1);
		expect(statuses.map((status) => status.state)).toEqual(["error"]);
		await vi.advanceTimersByTimeAsync(5_000);
		expect(statuses.map((status) => status.state)).toEqual(["error", "ready"]);
		await vi.advanceTimersByTimeAsync(29_999);
		expect(statuses).toHaveLength(2);
		await vi.advanceTimersByTimeAsync(1);
		expect(statuses).toHaveLength(3);
		stop();
	});

	it("stops polling once the listener unsubscribes", async () => {
		vi.useFakeTimers();
		fetchMock.mockImplementation(async () => notReadyResponse());
		const aoBridge = await loadBridge();
		const listener = vi.fn();

		const stop = aoBridge.daemon.onStatus(listener);
		await vi.advanceTimersByTimeAsync(5_000);
		expect(listener).toHaveBeenCalledTimes(1);
		stop();
		await vi.advanceTimersByTimeAsync(60_000);

		expect(listener).toHaveBeenCalledTimes(1);
		expect(fetchMock).toHaveBeenCalledTimes(1);
	});

	it("stops every readiness poll when the daemon status hook unmounts", async () => {
		vi.useFakeTimers();
		fetchMock.mockImplementation(async () => notReadyResponse());
		await loadBridge();
		const { useDaemonStatus } = await import("../hooks/useDaemonStatus");
		const queryClient = { invalidateQueries: vi.fn(), removeQueries: vi.fn(), setQueryData: vi.fn() } as unknown as QueryClient;

		const { unmount } = renderHook(() => useDaemonStatus(queryClient));
		await vi.advanceTimersByTimeAsync(10_000);
		expect(fetchMock).toHaveBeenCalled();
		unmount();
		const callsAtUnmount = fetchMock.mock.calls.length;
		await vi.advanceTimersByTimeAsync(120_000);

		expect(fetchMock).toHaveBeenCalledTimes(callsAtUnmount);
	});

	it("keeps the mock-data preview on a stopped daemon without probing", async () => {
		vi.stubEnv("VITE_NO_ELECTRON", "1");
		const aoBridge = await loadBridge();

		await expect(aoBridge.daemon.getStatus()).resolves.toMatchObject({ state: "stopped" });
		expect(fetchMock).not.toHaveBeenCalled();
	});
});

describe("web clipboard bridge", () => {
	const savedClipboard = Object.getOwnPropertyDescriptor(navigator, "clipboard");

	function stubClipboard(clipboard: Partial<Clipboard> | undefined) {
		Object.defineProperty(navigator, "clipboard", { configurable: true, value: clipboard });
	}

	afterEach(() => {
		if (savedClipboard) Object.defineProperty(navigator, "clipboard", savedClipboard);
		else delete (navigator as { clipboard?: unknown }).clipboard;
	});

	async function globalToasts() {
		const { useUiStore } = await import("../stores/ui-store");
		return useUiStore.getState().globalToasts;
	}

	it("returns the clipboard text when the browser allows the read", async () => {
		stubClipboard({ readText: vi.fn(async () => "hello") });
		const aoBridge = await loadBridge();

		await expect(aoBridge.clipboard.readText()).resolves.toBe("hello");
		expect(await globalToasts()).toEqual([]);
	});

	it("shows a toast and resolves empty when the read permission is denied", async () => {
		stubClipboard({ readText: vi.fn(async () => { throw new DOMException("Read permission denied.", "NotAllowedError"); }) });
		const aoBridge = await loadBridge();

		await expect(aoBridge.clipboard.readText()).resolves.toBe("");
		await expect(aoBridge.clipboard.readText()).resolves.toBe("");

		const toasts = await globalToasts();
		expect(toasts).toHaveLength(1);
		expect(toasts[0]).toMatchObject({ title: "Clipboard access is blocked", tone: "info" });
	});

	it("shows a toast when the page has no Clipboard API (plain HTTP)", async () => {
		stubClipboard(undefined);
		const aoBridge = await loadBridge();

		await expect(aoBridge.clipboard.readText()).resolves.toBe("");
		expect(await globalToasts()).toHaveLength(1);
	});
});

describe("web remotes bridge selection", () => {
	it("reads the hub BFF's host registry in the web build", async () => {
		vi.stubEnv("VITE_AO_WEB", "1");
		fetchMock.mockResolvedValueOnce(new Response(JSON.stringify({ hosts: [{ id: "h_worker", label: "Worker", url: "http://worker:3011", source: "manual", status: "online" }] }), { status: 200 }));
		const aoBridge = await loadBridge();

		await expect(aoBridge.remotes.list()).resolves.toEqual([{ hostId: "h_worker", label: "Worker", url: "http://worker:3011" }]);
		expect(fetchMock).toHaveBeenCalledWith("/hub/hosts", expect.anything());
	});

	it("keeps the mock-data preview without remote hosts", async () => {
		vi.stubEnv("VITE_NO_ELECTRON", "1");
		const aoBridge = await loadBridge();

		await expect(aoBridge.remotes.list()).resolves.toEqual([]);
		expect(fetchMock).not.toHaveBeenCalled();
	});
});
