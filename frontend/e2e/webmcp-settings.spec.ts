/// <reference path="../src/renderer/webmcp/global.d.ts" />
import { expect, test, type Page } from "@playwright/test";
import { installFakeBridge } from "./support/fake-bridge";

function settingsToolNames(page: Page): Promise<string[]> {
	return page.evaluate(() =>
		window.__aoWebMcp!.listTools()
			.map((tool) => tool.name)
			.filter((name) => name.startsWith("settings.")),
	);
}

test("@P0 settings tools drive the update download and close with Settings", async ({ page }) => {
	await installFakeBridge(page, { version: "1.0.0", updateStatus: { state: "available", version: "2.0.0" } });
	await page.goto("/#/settings");
	await expect(page.getByTestId("settings-page")).toBeVisible();
	await expect.poll(() => settingsToolNames(page)).toContain("settings.update.download");

	const read = await page.evaluate(() => window.__aoWebMcp!.callTool("settings.read")) as {
		sections: { id: string; label: string }[];
		updates: { state: string; version?: string } | null;
	};
	expect(read.sections.map((section) => section.id)).toContain("updates");
	for (const { label } of read.sections) {
		await expect(page.getByRole("button", { name: label, exact: true })).toBeVisible();
	}
	expect(read.updates).toMatchObject({ state: "available", version: "2.0.0" });

	await expect(
		page.evaluate(() => window.__aoWebMcp!.callTool("settings.select_section", { id: "updates" })),
	).resolves.toEqual({ activeSection: "updates" });
	await expect(page.locator('[data-testid="settings-section"][data-section="updates"]')).toBeVisible();

	await page.evaluate(() => window.__aoWebMcp!.callTool("settings.update.download"));
	await expect(
		page.evaluate(() => window.__aoWebMcp!.callTool("settings.wait_for_update_state", { state: "downloading" })),
	).resolves.toMatchObject({ state: "downloading" });
	await expect(page.getByTestId("update-status-line")).toContainText(/Starting download…|Downloading…/);

	await page.getByRole("button", { name: "Close settings" }).click();
	await expect(page.getByTestId("settings-page")).toHaveCount(0);
	await expect.poll(() => settingsToolNames(page)).toEqual([]);
});
