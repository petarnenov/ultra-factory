import { renderHook } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { boardKanbanColumnOrder, type KanbanColumnView } from "../../lib/session-presentation";
import type { WorkspaceSession } from "../../types/workspace";
import { listTools } from "../model-context";
import { type BoardAccess, type BoardContext, boardTools, readBoard, useBoardTools } from "./board";

const columns = boardKanbanColumnOrder.map(
	(column) => ({ column, label: column }) as KanbanColumnView,
);

function session(overrides: Partial<WorkspaceSession> & { id: string }): WorkspaceSession {
	return {
		title: `Task ${overrides.id}`,
		status: "working",
		updatedAt: "2026-10-23T00:00:00.000Z",
		...overrides,
	} as WorkspaceSession;
}

function context(sessions: WorkspaceSession[], overrides: Partial<BoardContext> = {}): BoardContext {
	return {
		projectId: "fake-proj",
		columns,
		sessions,
		openSession: vi.fn(),
		requestNewTask: vi.fn(),
		...overrides,
	};
}

// A board whose context and listeners the test drives like a re-render would.
function liveBoard(initial: BoardContext | undefined) {
	let current = initial;
	const listeners = new Set<() => void>();
	const access: BoardAccess = {
		current: () => current,
		subscribe: (listener) => {
			listeners.add(listener);
			return () => {
				listeners.delete(listener);
			};
		},
	};
	return {
		access,
		render(next: BoardContext | undefined) {
			current = next;
			for (const listener of listeners) listener();
		},
	};
}

afterEach(() => {
	vi.useRealTimers();
});

describe("readBoard", () => {
	it("lists every lane in order with the cards the lanes render", () => {
		const board = readBoard(
			context([
				session({ id: "building-1", status: "working", kanbanColumn: "building" }),
				session({ id: "review-1", status: "needs_input", kanbanColumn: "needs_review" }),
				session({ id: "ready-1", status: "mergeable", kanbanColumn: "ready" }),
			]),
		);

		expect(board.columns.map((lane) => lane.id)).toEqual(boardKanbanColumnOrder);
		expect(board.columns.map((lane) => lane.cards.map((card) => card.id))).toEqual([
			["building-1"],
			[],
			["review-1"],
			["ready-1"],
		]);
	});

	it("reports the derived lane, status and attention of each card", () => {
		const board = readBoard(context([session({ id: "blocked", status: "needs_input", kanbanColumn: "needs_review" })]));

		expect(board.columns[2].cards).toEqual([
			{ id: "blocked", title: "Task blocked", status: "needs_input", column: "needs_review", attention: true },
		]);
	});

	it("falls back to the status lane when the daemon sends no column", () => {
		const board = readBoard(context([session({ id: "legacy", status: "mergeable" })]));

		expect(board.columns[3].cards.map((card) => card.id)).toEqual(["legacy"]);
	});
});

describe("boardTools", () => {
	it("exposes the four board tools", () => {
		const tools = boardTools(liveBoard(context([])).access);

		expect([tools.read, tools.waitForCard, tools.openSession, tools.requestNewTask].map((tool) => tool.name)).toEqual([
			"board.read",
			"board.wait_for_card",
			"board.open_session",
			"board.request_new_task",
		]);
	});

	it("board.read returns the current board", async () => {
		const tools = boardTools(liveBoard(context([session({ id: "mover", kanbanColumn: "building" })])).access);

		const board = await tools.read.execute({});

		expect(board.columns[0].cards.map((card) => card.id)).toEqual(["mover"]);
	});

	it("board.wait_for_card resolves when a render moves the card into the column", async () => {
		const board = liveBoard(context([session({ id: "mover", kanbanColumn: "building" })]));
		const waiting = boardTools(board.access).waitForCard.execute({ sessionId: "mover", column: "ready" });

		board.render(context([session({ id: "mover", status: "mergeable", kanbanColumn: "ready" })]));

		await expect(waiting).resolves.toMatchObject({ id: "mover", column: "ready" });
	});

	it("board.wait_for_card resolves at once when the card is already there", async () => {
		const board = liveBoard(context([session({ id: "mover", kanbanColumn: "ready" })]));

		await expect(
			boardTools(board.access).waitForCard.execute({ sessionId: "mover", column: "ready" }),
		).resolves.toMatchObject({ id: "mover" });
	});

	it("board.wait_for_card rejects with precondition after timeoutMs", async () => {
		vi.useFakeTimers();
		const board = liveBoard(context([session({ id: "mover", kanbanColumn: "building" })]));
		const waiting = boardTools(board.access).waitForCard.execute({
			sessionId: "mover",
			column: "ready",
			timeoutMs: 1000,
		});
		const outcome = expect(waiting).rejects.toMatchObject({ code: "precondition" });

		await vi.advanceTimersByTimeAsync(1000);

		await outcome;
	});

	it("board.wait_for_card rejects when the board closes before the card arrives", async () => {
		const board = liveBoard(context([session({ id: "mover", kanbanColumn: "building" })]));
		const waiting = boardTools(board.access).waitForCard.execute({ sessionId: "mover", column: "ready" });

		board.render(undefined);

		await expect(waiting).rejects.toMatchObject({ code: "precondition" });
	});

	it("board.open_session opens a card on the board and rejects unknown ids", async () => {
		const openSession = vi.fn();
		const target = session({ id: "mover", kanbanColumn: "building" });
		const tools = boardTools(liveBoard(context([target], { openSession })).access);

		expect(await tools.openSession.execute({ sessionId: "mover" })).toEqual({ opened: "mover" });
		expect(openSession).toHaveBeenCalledWith(target);
		expect(() => tools.openSession.execute({ sessionId: "gone" })).toThrow(/is not on the board/);
	});

	it("board.request_new_task asks the board for a new task", async () => {
		const requestNewTask = vi.fn();
		const tools = boardTools(liveBoard(context([], { requestNewTask })).access);

		expect(await tools.requestNewTask.execute({})).toEqual({ opened: true });
		expect(requestNewTask).toHaveBeenCalledOnce();
	});
});

describe("useBoardTools", () => {
	it("registers the board tools while enabled and removes them on unmount", () => {
		const { unmount } = renderHook(() =>
			useBoardTools({
				scopeKey: "local:fake-proj",
				enabled: true,
				projectId: "fake-proj",
				columns,
				sessions: [],
				openSession: vi.fn(),
				requestNewTask: vi.fn(),
			}),
		);

		expect(listTools().map((tool) => tool.name)).toEqual([
			"board.open_session",
			"board.read",
			"board.request_new_task",
			"board.wait_for_card",
		]);

		unmount();

		expect(listTools().filter((tool) => tool.name.startsWith("board."))).toEqual([]);
	});

	it("registers nothing while the board is not interactive", () => {
		renderHook(() =>
			useBoardTools({
				scopeKey: "local:fake-proj",
				enabled: false,
				projectId: "fake-proj",
				columns,
				sessions: [],
				openSession: vi.fn(),
				requestNewTask: vi.fn(),
			}),
		);

		expect(listTools().filter((tool) => tool.name.startsWith("board."))).toEqual([]);
	});
});
