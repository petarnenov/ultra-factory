import { expect, test } from "@playwright/test";
import fs from "node:fs";
import path from "node:path";
import summary from "../src/renderer/benchmarks/data/summary.json";

test("benchmarks route, filters, and themes @P0", async ({ page }) => {
	const cspViolations: string[] = [];
	page.on("console", (message) => {
		if (/content security policy|violates the following directive/i.test(message.text())) {
			cspViolations.push(message.text());
		}
	});
	await page.goto("/#/benchmarks");
	await expect(page.getByRole("heading", { name: "Benchmarks" })).toBeVisible();
	await expect(page.locator('[aria-current="page"]', { hasText: "Benchmarks" })).toBeVisible();
	const scenarioCount = new Set(summary.map((row) => row.scenario)).size;
	await expect(page.getByTestId("benchmark-summary-row")).toHaveCount(scenarioCount);
	const sample = summary.find((row) => row.page === "board" && row.driver === "playwright") ?? summary[0];
	if (!sample) throw new Error("benchmark summary fixture has no rows");
	const boardRow = page.getByTestId("benchmark-summary-row").filter({ hasText: sample.scenario });
	const drivers = ["playwright", "webmcp-scripted", "webmcp-jev"] as const;
	for (const [column, driver] of drivers.entries()) {
		const metric = summary.find((row) => row.scenario === sample.scenario && row.driver === driver);
		const display = metric?.medianMs == null ? "—" : `${Math.round(metric.medianMs)} ms`;
		await expect(boardRow.getByRole("cell").nth(column + 1)).toContainText(display);
	}

	await page.getByRole("combobox", { name: "Page" }).click();
	await page.getByRole("option", { name: "Board" }).click();
	const boardScenarioCount = new Set(summary.filter((row) => row.page === "board").map((row) => row.scenario)).size;
	await expect(page.getByTestId("benchmark-summary-row")).toHaveCount(boardScenarioCount);
	await page.getByRole("switch", { name: "WebMCP + JEV" }).click();
	await expect(page.getByTestId("benchmark-driver-column-webmcp-jev")).toHaveCount(0);
	await expect(page.getByTestId("benchmark-summary-row").first().getByRole("cell")).toHaveCount(3);

	await page.goto("/#/");
	await page.getByRole("button", { name: "Search" }).click();
	const search = page.getByPlaceholder("Search projects, sessions, PRs, and commands…");
	await search.fill("benchmarks");
	await page.getByText("Benchmarks", { exact: true }).last().click();
	await expect(page.getByRole("heading", { name: "Benchmarks" })).toBeVisible();
	await expect(page.getByTestId("benchmark-summary-row")).toHaveCount(scenarioCount);

	const screenshotDirectory = path.resolve(process.cwd(), "e2e/screenshots/benchmarks");
	fs.mkdirSync(screenshotDirectory, { recursive: true });
	for (const theme of ["light", "dark"] as const) {
		await page.addInitScript((value) => localStorage.setItem("ao.theme", value), theme);
		for (const width of [1280, 390]) {
			await page.setViewportSize({ width, height: 900 });
			await page.reload();
			await expect(page.getByRole("heading", { name: "Benchmarks" })).toBeVisible();
			await expect(page.locator("html")).toHaveAttribute("data-theme", theme);
			const overflow = await page.evaluate(() => {
				const tables = Array.from(document.querySelectorAll<HTMLElement>("[data-slot='table-container']"))
					.filter((element) => element.scrollWidth > element.clientWidth + 1)
					.map((element) => ({
						label: element.querySelector("table")?.getAttribute("aria-label"),
						clientWidth: element.clientWidth,
						scrollWidth: element.scrollWidth,
					}));
				const elements = Array.from(document.querySelectorAll<HTMLElement>("body *"))
					.filter((element) => element.offsetWidth > 0 && element.scrollWidth > element.clientWidth + 2)
					.slice(0, 12)
					.map((element) => ({
						tag: element.tagName,
						className: typeof element.className === "string" ? element.className : "",
						clientWidth: element.clientWidth,
						scrollWidth: element.scrollWidth,
						text: element.textContent?.trim().slice(0, 80),
					}));
				return {
					document: document.documentElement.scrollWidth > document.documentElement.clientWidth,
					tables,
					elements,
				};
			});
			expect(overflow.document || overflow.tables.length > 0, `horizontal overflow at ${width}px in ${theme} theme: ${JSON.stringify(overflow)}`).toBe(false);
			await page.screenshot({ path: path.join(screenshotDirectory, `benchmarks-${theme}-${width}.png`), fullPage: true });
		}
	}
	expect(cspViolations).toEqual([]);
});
