import { expect, test } from "@playwright/test";

test("consultation history preserves fallback evidence and distinguishes unavailable states @P0", async ({ page }) => {
 await page.goto("/test/fixtures/consultations/index.html#/consultations");
 await page.getByRole("link", { name: /Consultation with missing evidence/ }).click();
 await expect(page.getByRole("heading", { name: "Consultation with missing evidence" })).toBeVisible();
 await expect(page.getByText(/0 observed ratification rounds · 1 proposal versions/)).toBeVisible();
 await page.locator("summary").filter({ hasText: "Fallback arbitration" }).click();
 await expect(page.getByRole("heading", { name: "Original arbiter ruling" })).toBeVisible();
 await expect(page.locator('a[href^="file:"]')).toHaveCount(0);
 await page.getByRole("link", { name: "empty", exact: true }).click();
 await expect(page.getByText(/No indexed consultations/)).toBeVisible();
 await page.getByRole("link", { name: "unavailable", exact: true }).click();
 await expect(page.getByRole("alert")).toHaveText(/index is unavailable/);
 await page.getByRole("link", { name: "unconfigured", exact: true }).click();
 await expect(page.getByRole("alert")).toHaveText(/not configured/);
});
