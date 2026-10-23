import { describe, expect, it, vi } from "vitest";
import type { ChatComposerHandle } from "../../components/chat/ChatComposer";
import type { ChatRetryControl } from "../../components/chat/ChatWorkspace";
import type { ConversationSnapshot } from "../../types/conversation";
import { ToolError } from "../model-context";
import { chatTools, type ChatToolContext } from "./chat";

type FakeComposer = {
	text: string;
	suggestion: string | null;
};

function fakeComposer(state: FakeComposer): ChatComposerHandle {
	return {
		focus: vi.fn(),
		isEmpty: () => state.text === "",
		isDisabled: () => false,
		setDraft: (text) => {
			state.text = text;
		},
		read: () => ({ text: state.text, focused: false, suggestion: state.suggestion }),
		acceptSuggestion: () => {
			if (state.suggestion === null || state.text !== "") return;
			state.text = state.suggestion;
			state.suggestion = null;
		},
		dismissSuggestion: () => {
			state.suggestion = null;
		},
		submit: vi.fn(async () => {}),
	};
}

function snapshotWith(overrides: Partial<ConversationSnapshot> = {}): ConversationSnapshot {
	return {
		sessionId: "session-1",
		controller: { state: "ready" },
		turns: [],
		items: [],
		...overrides,
	} as unknown as ConversationSnapshot;
}

function failedTurn(id: string) {
	return { id, state: "failed", providerTurnId: `provider-${id}`, errorMessage: "Agent crashed" };
}

function setup(options: {
	composer?: FakeComposer;
	snapshot?: ConversationSnapshot;
	retry?: ChatRetryControl;
	newWorkDisabled?: boolean;
} = {}) {
	const composerState: FakeComposer = options.composer ?? { text: "", suggestion: null };
	const composer = fakeComposer(composerState);
	let snapshot = options.snapshot ?? snapshotWith();
	const retryControl: ChatRetryControl = options.retry ?? { retry: vi.fn(async () => {}) };
	const ctx: ChatToolContext = {
		sessionId: "session-1",
		snapshot: () => snapshot,
		composer: () => composer,
		retryControl: () => retryControl,
		newWorkDisabled: () => options.newWorkDisabled ?? false,
	};
	const tools = Object.fromEntries(chatTools(ctx).map((descriptor) => [descriptor.name, descriptor]));
	return {
		composerState,
		retryControl,
		tools,
		setSnapshot: (next: ConversationSnapshot) => {
			snapshot = next;
		},
		call: (name: string, args: unknown = {}) => tools[name].execute(args),
	};
}

describe("chat.accept_suggestion", () => {
	it("inserts the suggestion into an empty draft and clears it", async () => {
		const { call, composerState } = setup({ composer: { text: "", suggestion: "Fix the test." } });

		expect(call("chat.accept_suggestion")).toEqual({ text: "Fix the test." });
		expect(composerState).toEqual({ text: "Fix the test.", suggestion: null });
	});

	it("throws a precondition error when the draft is not empty", () => {
		const { call, composerState } = setup({ composer: { text: "My draft", suggestion: "Fix the test." } });

		expect(() => call("chat.accept_suggestion")).toThrow(ToolError);
		expect(composerState.text).toBe("My draft");
	});

	it("throws a precondition error when no suggestion is showing", () => {
		const { call } = setup();

		expect(() => call("chat.accept_suggestion")).toThrow(ToolError);
	});
});

describe("chat.dismiss_suggestion", () => {
	it("dismisses the showing suggestion", () => {
		const { call, composerState } = setup({ composer: { text: "", suggestion: "Fix the test." } });

		call("chat.dismiss_suggestion");
		expect(composerState.suggestion).toBeNull();
	});
});

describe("chat.send", () => {
	it("refuses an empty draft", async () => {
		const { call } = setup();

		await expect(call("chat.send")).rejects.toMatchObject({ code: "precondition" });
	});
});

describe("chat.retry", () => {
	const humanPrompt = {
		kind: "message",
		id: "prompt-1",
		turnId: "turn-1",
		role: "user",
		origin: "human",
		text: "Fix the reconnect test",
		streaming: false,
	};
	const failed = snapshotWith({
		turns: [failedTurn("turn-1")] as ConversationSnapshot["turns"],
		items: [humanPrompt] as ConversationSnapshot["items"],
	});

	it("surfaces the daemon's rejection message", async () => {
		const retry = vi.fn(async () => {
			throw new Error("Turn turn-1 is no longer retryable");
		});
		const { call } = setup({ snapshot: failed, retry: { retry } });

		await expect(call("chat.retry", { turnId: "turn-1" })).resolves.toEqual({
			retryError: "Turn turn-1 is no longer retryable",
		});
		expect(retry).toHaveBeenCalledWith("turn-1");
	});

	it("does not retry a failed turn that has no human prompt", async () => {
		const retry = vi.fn(async () => {});
		const automated = snapshotWith({
			turns: [failedTurn("turn-1")] as ConversationSnapshot["turns"],
			items: [
				{ kind: "message", id: "m1", turnId: "turn-1", role: "user", origin: "automation", text: "Run checks", streaming: false },
			] as ConversationSnapshot["items"],
		});
		const { call } = setup({ snapshot: automated, retry: { retry } });

		await expect(call("chat.retry", { turnId: "turn-1" })).rejects.toMatchObject({ code: "precondition" });
		expect(retry).not.toHaveBeenCalled();
	});

	it("does not retry a turn that is not failed", async () => {
		const retry = vi.fn(async () => {});
		const { call } = setup({ snapshot: snapshotWith(), retry: { retry } });

		await expect(call("chat.retry", { turnId: "turn-1" })).rejects.toMatchObject({ code: "precondition" });
		expect(retry).not.toHaveBeenCalled();
	});

	it("does not retry while another turn is running", async () => {
		const retry = vi.fn(async () => {});
		const running = snapshotWith({
			turns: [failedTurn("turn-1"), { id: "turn-2", state: "running" }] as ConversationSnapshot["turns"],
			items: [humanPrompt] as ConversationSnapshot["items"],
		});
		const { call } = setup({ snapshot: running, retry: { retry } });

		await expect(call("chat.retry", { turnId: "turn-1" })).rejects.toMatchObject({ code: "precondition" });
		expect(retry).not.toHaveBeenCalled();
	});

	it("does not retry while new work is disabled", async () => {
		const retry = vi.fn(async () => {});
		const { call } = setup({ snapshot: failed, retry: { retry }, newWorkDisabled: true });

		await expect(call("chat.retry", { turnId: "turn-1" })).rejects.toMatchObject({ code: "precondition" });
		expect(retry).not.toHaveBeenCalled();
	});
});

describe("chat.wait_for", () => {
	it("resolves when the composer text matches", async () => {
		const { call, composerState } = setup();

		const waiting = call("chat.wait_for", { where: "composer_equals", value: "later", timeoutMs: 1_000 });
		composerState.text = "later";
		await expect(waiting).resolves.toEqual({});
	});

	it("resolves when a message sent after the call appears", async () => {
		const { call, setSnapshot } = setup({
			snapshot: snapshotWith({
				items: [{ kind: "message", id: "old", role: "user", text: "Ship it", streaming: false }] as ConversationSnapshot["items"],
			}),
		});

		const waiting = call("chat.wait_for", { where: "message_sent", value: "Ship it", timeoutMs: 1_000 });
		setSnapshot(snapshotWith({
			items: [
				{ kind: "message", id: "old", role: "user", text: "Ship it", streaming: false },
				{ kind: "message", id: "new", role: "user", text: "Ship it", streaming: false },
			] as ConversationSnapshot["items"],
		}));
		await expect(waiting).resolves.toEqual({});
	});

	it("fails after timeoutMs when the predicate never holds", async () => {
		const { call } = setup();

		await expect(call("chat.wait_for", { where: "suggestion_visible", timeoutMs: 50 })).rejects.toThrow(ToolError);
	});
});
