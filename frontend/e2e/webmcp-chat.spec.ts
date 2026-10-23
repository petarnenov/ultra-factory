import { expect, test, type Page } from "@playwright/test";
import { openChat, prompt } from "./support/open-chat";

async function callTool<T = unknown>(page: Page, name: string, args?: unknown): Promise<T> {
	return page.evaluate(
		([toolName, toolArgs]) => window.__aoWebMcp!.callTool(toolName, toolArgs) as Promise<T>,
		[name, args] as const,
	);
}

test("WebMCP chat tools accept the suggestion and send the draft @P0", async ({ page }) => {
	const { composer, sent, suggestion } = await openChat(page);
	await expect(suggestion).toHaveText(prompt);

	await callTool(page, "chat.accept_suggestion");
	const read = await callTool<{ composer: { text: string } }>(page, "chat.read");
	expect(read.composer.text).toBe(prompt);
	await expect(composer).toHaveText(prompt);

	await callTool(page, "chat.send");
	await expect.poll(() => sent).toEqual([prompt]);
});
