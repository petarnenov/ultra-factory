import { expect, test, type Page } from "@playwright/test";
import { installRetryConversation } from "./support/retry-conversation";

async function capture(page: Page, name: string) {
	const directory = process.env.AO_RETRY_EVIDENCE_DIR;
	if (directory) await page.screenshot({ path: `${directory}/${name}.png`, fullPage: true });
}

test("retry stays disabled while another turn is running @T0", async ({ page }) => {
	await installRetryConversation(page, true);
	const retry = page.getByRole("button", { name: "Retry this turn" });
	await capture(page, "retry-while-running");
	await expect(retry).toBeDisabled();
});

test("retry refusal is visible beside the failed turn @T0", async ({ page }) => {
	await installRetryConversation(page, false);
	await page.getByRole("button", { name: "Retry this turn" }).click();
	const refusal = page.getByText("stop the current turn before retrying this one");
	await expect(refusal).toContainText("stop the current turn before retrying this one");
	await capture(page, "retry-visible-error");
});
