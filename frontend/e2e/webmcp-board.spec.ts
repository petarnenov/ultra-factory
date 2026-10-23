/// <reference path="../src/renderer/webmcp/global.d.ts" />
import { expect, test } from "@playwright/test";
import { installFakeAgent } from "./support/fake-bridge";

const BOARD_TOOLS = ["board.open_session", "board.read", "board.request_new_task", "board.wait_for_card"];

const columnCard = (column: string, id: string) =>
	`[data-testid="board-column"][data-column="${column}"] [data-session-id="${id}"]`;

test("@P0 board tools are registered on the board and removed on settings", async ({ page }) => {
	await installFakeAgent(page);
	await page.goto("/#/projects/fake-proj");
	await expect(page.getByTestId("board")).toBeVisible();

	const onBoard = await page.evaluate(() =>
		window.__aoWebMcp!.listTools().map((tool) => tool.name).filter((name) => name.startsWith("board.")),
	);
	expect(onBoard).toEqual(BOARD_TOOLS);

	await page.goto("/#/settings");
	await expect.poll(() =>
		page.evaluate(() => window.__aoWebMcp!.listTools().filter((tool) => tool.name.startsWith("board."))),
	).toEqual([]);
});

test("@P0 board.read lists the same lanes and cards as the DOM", async ({ page }) => {
	await installFakeAgent(page, {
		workers: [
			{ id: "mover", title: "Wandering worker", status: "working" },
			{ id: "reviewer", title: "Reviewed worker", status: "needs_input" },
		],
	});
	await page.goto("/#/projects/fake-proj");
	await expect(page.locator(columnCard("building", "mover"))).toBeVisible();
	await expect(page.locator(columnCard("needs_review", "reviewer"))).toBeVisible();

	const read = await page.evaluate(() => window.__aoWebMcp!.callTool("board.read")) as {
		columns: { id: string; cards: { id: string }[] }[];
	};
	const domLanes = await page.evaluate(() =>
		Array.from(document.querySelectorAll<HTMLElement>('[data-testid="board-column"]')).map((lane) => ({
			id: lane.dataset.column,
			cards: Array.from(lane.querySelectorAll<HTMLElement>('[data-testid="board-session-card"]')).map(
				(card) => card.dataset.sessionId,
			),
		})),
	);

	expect(read.columns.map((lane) => lane.id)).toEqual(domLanes.map((lane) => lane.id));
	for (const [index, lane] of read.columns.entries()) {
		expect(lane.cards.map((card) => card.id).sort()).toEqual([...domLanes[index].cards].sort());
	}
});

test("@P0 board.wait_for_card resolves once the card reaches the column", async ({ page }) => {
	await installFakeAgent(page, { workers: [{ id: "mover", title: "Wandering worker", status: "working" }] });
	await page.goto("/#/projects/fake-proj");
	await expect(page.locator(columnCard("building", "mover"))).toBeVisible();

	const waiting = page.evaluate(() =>
		window.__aoWebMcp!.callTool("board.wait_for_card", { sessionId: "mover", column: "ready" }),
	);
	await page.evaluate(() => window.__aoFakeAgent!.setStatus("mover", "mergeable", "idle"));

	await expect(waiting).resolves.toMatchObject({ id: "mover", column: "ready" });
	await expect(page.locator(columnCard("ready", "mover"))).toBeVisible();
});
