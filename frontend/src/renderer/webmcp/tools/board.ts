import { useEffect, useMemo, useRef } from "react";
import { boardSessionNeedsAttention, toKanbanColumn } from "@aoagents/product-ui";
import { boardKanbanColumnOrder, type KanbanColumnView } from "../../lib/session-presentation";
import type { WorkspaceSession } from "../../types/workspace";
import { ToolError, type ToolDescriptor } from "../model-context";
import { useModelContextTool } from "../useModelContextTool";

const DEFAULT_WAIT_TIMEOUT_MS = 5000;

export type BoardCard = {
	id: string;
	title: string;
	status: string;
	column: string;
	attention: boolean;
};

export type BoardRead = {
	projectId?: string;
	columns: { id: string; label: string; cards: BoardCard[] }[];
};

// What the board renders from. `sessions` is the final lane view, after the
// archive filter and the no_signal -> building mapping.
export type BoardContext = {
	projectId?: string;
	columns: KanbanColumnView[];
	sessions: WorkspaceSession[];
	openSession: (session: WorkspaceSession) => void;
	requestNewTask: () => void;
};

// `current` is undefined once the board is unmounted or hidden. Waiters
// re-read through it on every notification, never from a captured snapshot.
export type BoardAccess = {
	current: () => BoardContext | undefined;
	subscribe: (listener: () => void) => () => void;
};

export type BoardTools = {
	read: ToolDescriptor<unknown, BoardRead>;
	waitForCard: ToolDescriptor<WaitForCardArgs, BoardCard>;
	openSession: ToolDescriptor<{ sessionId: string }, { opened: string }>;
	requestNewTask: ToolDescriptor<unknown, { opened: true }>;
};

type WaitForCardArgs = { sessionId: string; column: string; timeoutMs?: number };

export function readBoard(context: BoardContext): BoardRead {
	return {
		projectId: context.projectId,
		columns: context.columns.map((column) => ({
			id: column.column,
			label: column.label,
			cards: context.sessions.filter((session) => laneOf(session) === column.column).map(toCard),
		})),
	};
}

export function boardTools(access: BoardAccess): BoardTools {
	return {
		read: {
			name: "board.read",
			description: "Return board columns and their cards.",
			inputSchema: { type: "object" },
			execute: () => readBoard(requireContext(access)),
		},
		waitForCard: {
			name: "board.wait_for_card",
			description: "Resolve when a card is in the given column, or fail after timeoutMs.",
			inputSchema: {
				type: "object",
				required: ["sessionId", "column"],
				properties: {
					sessionId: { type: "string" },
					column: { type: "string", enum: boardKanbanColumnOrder },
					timeoutMs: { type: "number" },
				},
			},
			execute: ({ sessionId, column, timeoutMs = DEFAULT_WAIT_TIMEOUT_MS }) =>
				waitForCard(access, sessionId, column, timeoutMs),
		},
		openSession: {
			name: "board.open_session",
			description: "Open a session card.",
			inputSchema: {
				type: "object",
				required: ["sessionId"],
				properties: { sessionId: { type: "string" } },
			},
			execute: ({ sessionId }) => {
				const context = requireContext(access);
				const session = context.sessions.find((candidate) => candidate.id === sessionId);
				if (!session) throw new ToolError("precondition", `${sessionId} is not on the board`);
				context.openSession(session);
				return { opened: sessionId };
			},
		},
		requestNewTask: {
			name: "board.request_new_task",
			description: "Open the New task dialog for this project.",
			inputSchema: { type: "object" },
			execute: () => {
				requireContext(access).requestNewTask();
				return { opened: true };
			},
		},
	};
}

type BoardToolsInput = BoardContext & {
	/** Identifies the host and project scope; a change re-registers the tools. */
	scopeKey: string;
	/** False while the board is not the interactive surface (e.g. starting). */
	enabled: boolean;
};

// Registers the board tools while the board is mounted and interactive.
export function useBoardTools({ scopeKey, enabled, ...context }: BoardToolsInput): void {
	const current: BoardContext | undefined = enabled ? context : undefined;
	const latest = useRef(current);
	const listeners = useRef(new Set<() => void>());
	latest.current = current;

	const notify = () => {
		for (const listener of listeners.current) listener();
	};

	// Runs after each board commit, so waiters read the rendered lanes.
	useEffect(notify, [context.sessions]);

	useEffect(() => {
		latest.current = current;
		return () => {
			// Teardown on scope change or unmount: waiters observe the closed board.
			latest.current = undefined;
			notify();
		};
	}, [scopeKey]);

	const access = useMemo<BoardAccess>(
		() => ({
			current: () => latest.current,
			subscribe: (listener) => {
				listeners.current.add(listener);
				return () => {
					listeners.current.delete(listener);
				};
			},
		}),
		[],
	);
	const tools = useMemo(() => boardTools(access), [access, scopeKey]);

	useModelContextTool(enabled ? tools.read : null, [enabled, tools]);
	useModelContextTool(enabled ? tools.waitForCard : null, [enabled, tools]);
	useModelContextTool(enabled ? tools.openSession : null, [enabled, tools]);
	useModelContextTool(enabled ? tools.requestNewTask : null, [enabled, tools]);
}

function laneOf(session: WorkspaceSession): string {
	return toKanbanColumn(session.kanbanColumn, session.status);
}

function toCard(session: WorkspaceSession): BoardCard {
	return {
		id: session.id,
		title: session.title,
		status: session.status,
		column: laneOf(session),
		attention: boardSessionNeedsAttention(session),
	};
}

function requireContext(access: BoardAccess): BoardContext {
	const context = access.current();
	if (!context) throw new ToolError("precondition", "the board is not visible");
	return context;
}

function findCardIn(context: BoardContext, sessionId: string, column: string): BoardCard | undefined {
	const session = context.sessions.find((candidate) => candidate.id === sessionId);
	return session && laneOf(session) === column ? toCard(session) : undefined;
}

function waitForCard(
	access: BoardAccess,
	sessionId: string,
	column: string,
	timeoutMs: number,
): Promise<BoardCard> {
	return new Promise<BoardCard>((resolve, reject) => {
		let unsubscribe = () => {};
		const settle = () => {
			clearTimeout(timer);
			unsubscribe();
		};
		const timer = setTimeout(() => {
			settle();
			reject(new ToolError("precondition", `${sessionId} is not in ${column} after ${timeoutMs}ms`));
		}, timeoutMs);
		const check = () => {
			const context = access.current();
			if (!context) {
				settle();
				reject(new ToolError("precondition", "the board closed before the card arrived"));
				return;
			}
			const card = findCardIn(context, sessionId, column);
			if (!card) return;
			settle();
			resolve(card);
		};
		unsubscribe = access.subscribe(check);
		check();
	});
}
