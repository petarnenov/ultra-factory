import type { ReactNode } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { ToolDescriptor } from "./model-context";

type Model = typeof import("./model-context");

// The native implementation is detected at module load, so each test loads a
// fresh module after installing (or not) its stub.
async function load(options: { dev?: boolean; preview?: boolean } = {}): Promise<Model> {
	vi.resetModules();
	vi.stubEnv("DEV", options.dev ?? true);
	vi.doMock("../lib/preview-mode", () => ({ usesPreviewWorkspaceData: options.preview ?? false }));
	return import("./model-context");
}

function tool(overrides: Partial<ToolDescriptor<any, any>> = {}): ToolDescriptor<any, any> {
	return {
		name: "board.read",
		description: "Read the board lanes.",
		inputSchema: { type: "object" },
		execute: () => ({ ok: true }),
		...overrides,
	};
}

function stubNative(stub: unknown): void {
	Object.defineProperty(document, "modelContext", { configurable: true, writable: true, value: stub });
}

afterEach(() => {
	delete document.modelContext;
	delete window.__aoWebMcp;
	vi.doUnmock("../lib/preview-mode");
	vi.unstubAllEnvs();
	vi.restoreAllMocks();
});

describe("registry", () => {
	it("lists tools sorted by name and unregisters through the cleanup", async () => {
		const model = await load();
		const cleanupB = model.registerTool(tool({ name: "lane.move" }));
		model.registerTool(tool({ name: "board.read" }));

		expect(model.listTools().map((t) => t.name)).toEqual(["board.read", "lane.move"]);
		cleanupB();
		expect(model.listTools().map((t) => t.name)).toEqual(["board.read"]);
	});

	it("replaces a same-name registration and warns in DEV", async () => {
		const warn = vi.spyOn(console, "warn").mockImplementation(() => undefined);
		const model = await load({ dev: true });
		model.registerTool(tool({ description: "first" }));
		model.registerTool(tool({ description: "second" }));

		expect(model.listTools()).toHaveLength(1);
		expect(model.listTools()[0].description).toBe("second");
		expect(warn).toHaveBeenCalledWith(expect.stringContaining("board.read"));
	});

	it("does not warn on replacement outside DEV", async () => {
		const warn = vi.spyOn(console, "warn").mockImplementation(() => undefined);
		const model = await load({ dev: false });
		model.registerTool(tool());
		model.registerTool(tool({ description: "again" }));

		expect(warn).not.toHaveBeenCalled();
	});

	it("keeps a newer same-name registration when a stale cleanup runs", async () => {
		const model = await load();
		const staleCleanup = model.registerTool(tool({ description: "old" }));
		model.registerTool(tool({ description: "new" }));

		staleCleanup();

		expect(model.listTools()[0].description).toBe("new");
	});

	it("supports StrictMode-style register, cleanup, register", async () => {
		const model = await load();
		const cleanup = model.registerTool(tool());
		cleanup();
		model.registerTool(tool());

		expect(model.listTools().map((t) => t.name)).toEqual(["board.read"]);
	});
});

describe("native forwarding", () => {
	it("forwards register, replace, and unregister to a stubbed document.modelContext", async () => {
		const native = { registerTool: vi.fn(), unregisterTool: vi.fn() };
		stubNative(native);
		const model = await load();

		const staleCleanup = model.registerTool(tool());
		expect(native.registerTool).toHaveBeenCalledTimes(1);
		expect(native.registerTool.mock.calls[0][0].name).toBe("board.read");

		const replacementCleanup = model.registerTool(tool({ description: "replaced" }));
		expect(native.unregisterTool).toHaveBeenCalledWith("board.read");
		expect(native.registerTool).toHaveBeenCalledTimes(2);

		// A stale cleanup must not touch the newer native or local registration.
		staleCleanup();
		expect(native.unregisterTool).toHaveBeenCalledTimes(1);
		expect(model.listTools()).toHaveLength(1);

		replacementCleanup();
		expect(native.unregisterTool).toHaveBeenCalledTimes(2);
		expect(model.listTools()).toHaveLength(0);
	});

	it("does not replace a native document.modelContext with the shim", async () => {
		const native = { registerTool: vi.fn(), unregisterTool: vi.fn() };
		stubNative(native);
		const model = await load();

		model.installModelContext();

		expect(document.modelContext).toBe(native);
	});

	it("keeps the local registry when native registration throws or rejects", async () => {
		const warn = vi.spyOn(console, "warn").mockImplementation(() => undefined);
		stubNative({
			registerTool: vi.fn(() => {
				throw new Error("duplicate");
			}),
			unregisterTool: vi.fn(() => Promise.reject(new Error("gone"))),
		});
		const model = await load();

		model.registerTool(tool());
		model.unregisterTool("board.read");
		await new Promise((resolve) => setTimeout(resolve, 0));

		expect(warn).toHaveBeenCalledTimes(2);
		expect(model.listTools()).toHaveLength(0);
	});

	it("ignores a native object that lacks registerTool", async () => {
		stubNative({ registerTool: undefined });
		const model = await load();
		model.installModelContext();

		expect(document.modelContext?.registerTool).toBeTypeOf("function");
	});

	it("detects the legacy navigator.modelContext for native forwarding only", async () => {
		const native = { registerTool: vi.fn(), unregisterTool: vi.fn() };
		vi.stubGlobal("navigator", { ...navigator, modelContext: native });
		const model = await load();
		model.registerTool(tool());

		expect(native.registerTool).toHaveBeenCalledTimes(1);
		vi.unstubAllGlobals();
	});
});

describe("callTool", () => {
	it("rejects unknown tools with unknown_tool", async () => {
		const model = await load();

		await expect(model.callTool("missing.tool")).rejects.toMatchObject({ code: "unknown_tool" });
	});

	it("runs execute with valid args and returns its result", async () => {
		const model = await load();
		model.registerTool(
			tool({
				inputSchema: { type: "object", properties: { section: { type: "string" } }, required: ["section"] },
				execute: (args: { section: string }) => ({ section: args.section }),
			}),
		);

		await expect(model.callTool("board.read", { section: "general" })).resolves.toEqual({ section: "general" });
	});

	it("rejects invalid args with invalid_args before execute runs", async () => {
		const model = await load();
		const execute = vi.fn(() => ({ ok: true }));
		model.registerTool(
			tool({
				inputSchema: {
					type: "object",
					properties: {
						timeout: { type: "number" },
						section: { type: "string", enum: ["general", "advanced"] },
						tags: { type: "array" },
						options: { type: "object" },
						enabled: { type: "boolean" },
					},
					required: ["timeout"],
				},
				execute,
			}),
		);

		const invalid: unknown[] = [
			undefined,
			null,
			[],
			{},
			{ timeout: "5" },
			{ timeout: NaN },
			{ timeout: Infinity },
			{ timeout: 1, section: "other" },
			{ timeout: 1, section: 3 },
			{ timeout: 1, tags: {} },
			{ timeout: 1, options: [] },
			{ timeout: 1, options: null },
			{ timeout: 1, enabled: "yes" },
		];
		for (const args of invalid) {
			await expect(model.callTool("board.read", args), JSON.stringify(args)).rejects.toMatchObject({
				code: "invalid_args",
			});
		}
		expect(execute).not.toHaveBeenCalled();
	});

	it("accepts omitted optional properties", async () => {
		const model = await load();
		model.registerTool(
			tool({
				inputSchema: { type: "object", properties: { timeout: { type: "number" } } },
			}),
		);

		await expect(model.callTool("board.read")).resolves.toEqual({ ok: true });
	});
});

describe("installModelContext surface gating", () => {
	it("installs the shim and window.__aoWebMcp in DEV", async () => {
		const model = await load({ dev: true });
		model.installModelContext();

		expect(typeof document.modelContext?.registerTool).toBe("function");
		expect(window.__aoWebMcp?.listTools).toBeTypeOf("function");
	});

	it("installs the shim but hides window.__aoWebMcp outside DEV and preview", async () => {
		const model = await load({ dev: false, preview: false });
		model.installModelContext();

		expect(typeof document.modelContext?.registerTool).toBe("function");
		expect(window.__aoWebMcp).toBeUndefined();
	});

	it("exposes window.__aoWebMcp in preview workspace builds", async () => {
		const model = await load({ dev: false, preview: true });
		model.installModelContext();

		expect(window.__aoWebMcp?.callTool).toBeTypeOf("function");
	});

	it("keeps the shim and registrations working after repeated installation", async () => {
		const model = await load({ dev: true });
		model.installModelContext();
		model.registerTool(tool());
		model.installModelContext();

		document.modelContext!.registerTool(tool({ name: "lane.move" }));

		expect(model.listTools().map((t) => t.name)).toEqual(["board.read", "lane.move"]);
		expect(window.__aoWebMcp!.listTools().map((t) => t.name)).toEqual(["board.read", "lane.move"]);
	});
});

describe("useModelContextTool", () => {
	it("unregisters on unmount under StrictMode and lets an in-flight call resolve", async () => {
		const model = await load({ dev: true });
		// Re-import after resetModules so the hook shares one React copy.
		const { createElement, StrictMode } = await import("react");
		const { renderHook, act } = await import("@testing-library/react");
		const { useModelContextTool } = await import("./useModelContextTool");

		let release!: (value: unknown) => void;
		const execute = vi.fn(() => new Promise<unknown>((resolve) => (release = resolve)));
		const descriptor = tool({ execute });
		const wrapper = (props: { children?: ReactNode }) => createElement(StrictMode, null, props.children);
		const { unmount } = renderHook(() => useModelContextTool(descriptor, [descriptor]), { wrapper });

		expect(model.listTools().map((t) => t.name)).toEqual(["board.read"]);

		let pending!: Promise<unknown>;
		act(() => {
			pending = model.callTool("board.read");
		});
		expect(execute).toHaveBeenCalledTimes(1);

		unmount();
		expect(model.listTools()).toHaveLength(0);
		await expect(model.callTool("board.read")).rejects.toMatchObject({ code: "unknown_tool" });

		release({ ok: true });
		await expect(pending).resolves.toEqual({ ok: true });
	});
});
