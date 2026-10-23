import type { ChatComposerHandle } from "../../components/chat/ChatComposer";
import type { ChatRetryControl } from "../../components/chat/ChatWorkspace";
import { apiErrorMessage } from "../../lib/api-client";
import { activeTurn, retryableTurnIds, type ConversationSnapshot } from "../../types/conversation";
import { ToolError, type ToolDescriptor } from "../model-context";

export type ChatRead = {
	sessionId: string;
	controller: string;
	composer: { text: string; focused: boolean; suggestion: string | null };
	turns: { id: string; state: string; errorMessage?: string; retryError?: string }[];
	messages: { id: string; turnId?: string; role: string; text: string; streaming: boolean }[];
};

export type ChatToolContext = {
	sessionId: string;
	// Each getter reads the state the UI renders from at call time.
	snapshot: () => ConversationSnapshot | undefined;
	composer: () => ChatComposerHandle | null;
	retryControl: () => ChatRetryControl | undefined;
	newWorkDisabled: () => boolean;
};

const WAIT_TIMEOUT_MS = 5_000;
const WAIT_POLL_MS = 25;

const WAIT_PREDICATES = [
	"suggestion_visible",
	"suggestion_hidden",
	"composer_equals",
	"retry_error_visible",
	"message_sent",
] as const;

export function chatTools(ctx: ChatToolContext): ToolDescriptor[] {
	return [
		tool({
			name: "chat.read",
			description: "Return conversation turns, messages, composer text and the prompt suggestion.",
			inputSchema: { type: "object" },
			execute: () => readChat(ctx),
		}),
		tool({
			name: "chat.type",
			description: "Replace the composer draft with text.",
			inputSchema: { type: "object", required: ["text"], properties: { text: { type: "string" } } },
			execute: ({ text }: { text: string }) => {
				mountedComposer(ctx).setDraft(text);
				return { text };
			},
		}),
		tool({
			name: "chat.accept_suggestion",
			description: "Insert the current prompt suggestion into the empty composer.",
			inputSchema: { type: "object" },
			execute: () => {
				const composer = mountedComposer(ctx);
				const { text, suggestion } = composer.read();
				if (suggestion === null) throw new ToolError("precondition", "no prompt suggestion is showing");
				if (text !== "") throw new ToolError("precondition", "the composer draft is not empty");
				composer.acceptSuggestion();
				return { text: composer.read().text };
			},
		}),
		tool({
			name: "chat.dismiss_suggestion",
			description: "Dismiss the current prompt suggestion.",
			inputSchema: { type: "object" },
			execute: () => {
				const composer = mountedComposer(ctx);
				if (composer.read().suggestion === null) throw new ToolError("precondition", "no prompt suggestion is showing");
				composer.dismissSuggestion();
				return {};
			},
		}),
		tool({
			name: "chat.send",
			description: "Send the composer draft.",
			inputSchema: { type: "object" },
			execute: async () => {
				const composer = mountedComposer(ctx);
				if (composer.isDisabled()) throw new ToolError("precondition", "the composer is disabled");
				if (composer.isEmpty()) throw new ToolError("precondition", "the composer draft is empty");
				await composer.submit();
				return {};
			},
		}),
		tool({
			name: "chat.retry",
			description: "Retry a failed turn.",
			inputSchema: { type: "object", required: ["turnId"], properties: { turnId: { type: "string" } } },
			execute: async ({ turnId }: { turnId: string }) => {
				assertRetryable(ctx, turnId);
				try {
					await ctx.retryControl()?.retry(turnId);
					return { retryError: null };
				} catch (error) {
					return { retryError: apiErrorMessage(error) };
				}
			},
		}),
		tool({
			name: "chat.wait_for",
			description: "Resolve when a predicate on chat state holds, or fail after timeoutMs.",
			inputSchema: {
				type: "object",
				required: ["where"],
				properties: {
					where: { type: "string", enum: [...WAIT_PREDICATES] },
					value: { type: "string" },
					timeoutMs: { type: "number" },
				},
			},
			execute: async ({ where, value, timeoutMs = WAIT_TIMEOUT_MS }: WaitArgs) => {
				await waitUntil(waitPredicate(ctx, where, value), timeoutMs);
				return {};
			},
		}),
	];
}

// The registry stores descriptors with unknown arguments; each tool
// narrows its own.
function tool<A, R>(descriptor: ToolDescriptor<A, R>): ToolDescriptor {
	return descriptor as ToolDescriptor;
}

export type ChatWaitPredicate = (typeof WAIT_PREDICATES)[number];
export type WaitArgs = { where: ChatWaitPredicate; value?: string; timeoutMs?: number };

function mountedComposer(ctx: ChatToolContext): ChatComposerHandle {
	const composer = ctx.composer();
	if (!composer) throw new ToolError("precondition", "the chat composer is not mounted");
	return composer;
}

// Same eligibility as the Retry button: a retryable failed turn, with no
// turn in flight, no retry already pending, and new work allowed.
function assertRetryable(ctx: ChatToolContext, turnId: string): void {
	const snapshot = ctx.snapshot();
	const retry = ctx.retryControl();
	if (!snapshot || !retry || ctx.newWorkDisabled()) throw new ToolError("precondition", "retry is not available");
	if (!retryableTurnIds(snapshot).has(turnId)) throw new ToolError("precondition", `turn ${turnId} is not retryable`);
	if (activeTurn(snapshot) || retry.pending) throw new ToolError("precondition", "another turn is in flight");
}

function readChat(ctx: ChatToolContext): ChatRead {
	const snapshot = ctx.snapshot();
	const retry = ctx.retryControl();
	return {
		sessionId: ctx.sessionId,
		controller: snapshot?.controller.state ?? "unknown",
		composer: ctx.composer()?.read() ?? { text: "", focused: false, suggestion: null },
		turns: (snapshot?.turns ?? []).map((turn) => ({
			id: turn.id,
			state: turn.state,
			errorMessage: turn.errorMessage,
			retryError: retry?.turnId === turn.id ? retry.error : undefined,
		})),
		messages: (snapshot?.items ?? []).flatMap((item) =>
			item.kind === "message"
				? [{ id: item.id, turnId: item.turnId, role: item.role, text: item.text, streaming: Boolean(item.streaming) }]
				: [],
		),
	};
}

function waitPredicate(ctx: ChatToolContext, where: WaitArgs["where"], value: string | undefined): () => boolean {
	switch (where) {
		case "suggestion_visible":
			return () => ctx.composer()?.read().suggestion != null;
		case "suggestion_hidden":
			return () => ctx.composer()?.read().suggestion == null;
		case "composer_equals": {
			const text = requiredValue(where, value);
			return () => ctx.composer()?.read().text === text;
		}
		case "retry_error_visible":
			return () => ctx.retryControl()?.error !== undefined;
		case "message_sent": {
			// Only a message sent after this call counts, not one already in history.
			const text = requiredValue(where, value);
			const sentBefore = countUserMessages(ctx, text);
			return () => countUserMessages(ctx, text) > sentBefore;
		}
	}
}

function requiredValue(where: WaitArgs["where"], value: string | undefined): string {
	if (value === undefined) throw new ToolError("invalid_args", `${where} requires "value"`);
	return value;
}

function countUserMessages(ctx: ChatToolContext, text: string): number {
	return (ctx.snapshot()?.items ?? []).filter(
		(item) => item.kind === "message" && item.role === "user" && item.text === text,
	).length;
}

function waitUntil(predicate: () => boolean, timeoutMs: number): Promise<void> {
	return new Promise((resolve, reject) => {
		const deadline = Date.now() + timeoutMs;
		const check = () => {
			if (predicate()) {
				clearInterval(timer);
				resolve();
			} else if (Date.now() >= deadline) {
				clearInterval(timer);
				reject(new ToolError("precondition", `timed out after ${timeoutMs}ms`));
			}
		};
		const timer = setInterval(check, WAIT_POLL_MS);
		check();
	});
}
