import { expect, test } from "@playwright/test";
import { listToolNames, waitForTools } from "./webmcp/assert";
import { createHostState, scenarios } from "./webmcp/scenarios";

for (const scenario of scenarios) {
	test(`@P0 ${scenario.id} tools are registered on its route`, async ({ page }) => {
		const host = createHostState();
		await scenario.setup(page, host);
		await page.goto(scenario.route);
		const prefix = scenario.page === "board" ? "board." : scenario.page === "settings" ? "settings." : "chat.";
		await waitForTools(page, prefix);
		const registered = await listToolNames(page);
		const required = [...new Set(scenario.tools.filter((step) => step.kind === "tool").map((step) => step.name))];
		expect(registered, `${scenario.id} tool catalog`).toEqual(expect.arrayContaining(required));
	});
}
