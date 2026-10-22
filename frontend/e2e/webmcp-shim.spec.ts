/// <reference path="../src/renderer/webmcp/global.d.ts" />
import { expect, test } from "@playwright/test";
import { installFakeAgent } from "./support/fake-bridge";

test("@P0 the board exposes document.modelContext and the __aoWebMcp test surface", async ({ page }) => {
	await installFakeAgent(page);
	await page.goto("/#/");

	const surface = await page.evaluate(() => ({
		hasRegisterTool: typeof document.modelContext?.registerTool === "function",
		tools: window.__aoWebMcp!.listTools(),
	}));

	expect(surface.hasRegisterTool).toBe(true);
	expect(Array.isArray(surface.tools)).toBe(true);
});

test("@P0 registered tools are listed and invalid args are rejected before execute", async ({ page }) => {
	await installFakeAgent(page);
	await page.goto("/#/");

	const result = await page.evaluate(async () => {
		let executed = false;
		document.modelContext!.registerTool({
			name: "shim.echo",
			description: "Echo the requested count.",
			inputSchema: { type: "object", properties: { count: { type: "number" } }, required: ["count"] },
			execute: (args) => {
				executed = true;
				return args;
			},
		});
		const listed = window.__aoWebMcp!.listTools().map((tool) => tool.name);
		const echoed = await window.__aoWebMcp!.callTool("shim.echo", { count: 2 });
		const executedBeforeInvalid = executed;
		let code: string | undefined;
		try {
			await window.__aoWebMcp!.callTool("shim.echo", { count: "2" });
		} catch (error) {
			code = (error as { code?: string }).code;
		}
		return { listed, echoed, code, executedByInvalidCall: executed !== executedBeforeInvalid };
	});

	expect(result.listed).toContain("shim.echo");
	expect(result.echoed).toEqual({ count: 2 });
	expect(result.code).toBe("invalid_args");
	expect(result.executedByInvalidCall).toBe(false);
});
