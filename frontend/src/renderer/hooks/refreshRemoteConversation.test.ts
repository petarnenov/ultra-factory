import { InfiniteData, QueryClient } from "@tanstack/react-query";
import { beforeEach, describe, expect, it, vi } from "vitest";

const { remoteGetMock, apiErrorCodeMock } = vi.hoisted(() => ({
	remoteGetMock: vi.fn(),
	apiErrorCodeMock: vi.fn(),
}));

vi.mock("../lib/host-clients", async (importOriginal) => ({
	...await importOriginal<typeof import("../lib/host-clients")>(),
	clientForSessionHost: () => ({ GET: remoteGetMock }),
}));
vi.mock("../lib/api-client", async (importOriginal) => ({
	...await importOriginal<typeof import("../lib/api-client")>(),
	apiErrorCode: apiErrorCodeMock,
}));

import type { ConversationSnapshot } from "../types/conversation";
import { conversationQueryKey, refreshRemoteConversation } from "./useConversation";

const msg = (id: string, sequence: number, text: string, turnId = "t1") => ({
	kind: "message", id, turnId, sequence, revision: 0, role: "assistant", origin: "provider",
	text, streaming: false, createdAt: "2026-10-20T12:00:00Z",
});
const activity = (id: string, sequence: number, status: string) => ({
	kind: "activity", id, turnId: "t1", sequence, revision: 0, activityKind: "command", status,
	summary: "ls", createdAt: "2026-10-20T12:00:00Z",
});
const turn = (id: string, extra: object = {}) => ({ id, state: "running", requestedAt: `2026-10-20T12:00:0${id.slice(1)}Z`, ...extra });
const wire = (extra: object) => ({
	conversationId: "c1", sessionId: "s1", mode: "chat", controller: "ready", activeBranchId: "b1",
	hasMoreBefore: false, nativeForkAvailableAfterSequence: 0, settings: {}, turns: [], messages: [], activities: [],
	...extra,
});

const FULL_A = wire({ latestSequence: 3, oldestSequence: 1, revision: 100, turns: [turn("t1")], messages: [msg("m1", 1, "hi"), msg("m2", 3, "a")], activities: [activity("a1", 2, "running")] });
const FULL_B = wire({
	latestSequence: 4, oldestSequence: 1, revision: 200, turns: [turn("t1"), turn("t2")],
	messages: [msg("m1", 1, "hi"), msg("m2", 3, "ab"), msg("m3", 4, "new", "t2")], activities: [activity("a1", 2, "completed")],
});
const DELTA_B = wire({
	latestSequence: 4, oldestSequence: 1, revision: 200, delta: true, turns: [turn("t1"), turn("t2")],
	messages: [msg("m2", 3, "ab"), msg("m3", 4, "new", "t2")], activities: [activity("a1", 2, "completed")],
});

const live = (client: QueryClient) =>
	client.getQueryData<InfiniteData<ConversationSnapshot>>(conversationQueryKey("s1", "h1"))?.pages[0];

async function seeded(first: object): Promise<QueryClient> {
	const client = new QueryClient();
	remoteGetMock.mockResolvedValueOnce({ data: first, response: { status: 200 } });
	await refreshRemoteConversation(client, "s1", "h1");
	return client;
}

describe("refreshRemoteConversation cursor", () => {
	beforeEach(() => {
		remoteGetMock.mockReset();
		apiErrorCodeMock.mockReset();
	});

	it("sends the cached revision and merges a delta to the same state as a full refetch", async () => {
		const viaDelta = await seeded(FULL_A);
		remoteGetMock.mockResolvedValueOnce({ data: DELTA_B, response: { status: 200 } });
		await refreshRemoteConversation(viaDelta, "s1", "h1");
		expect(remoteGetMock.mock.calls[1][1].params.query.sinceRevision).toBe(100);

		const viaFull = await seeded(FULL_A);
		remoteGetMock.mockResolvedValueOnce({ data: FULL_B, response: { status: 200 } });
		// Force the full path: the host answers without the delta flag.
		await refreshRemoteConversation(viaFull, "s1", "h1");
		expect(live(viaDelta)).toEqual(live(viaFull));
		expect(live(viaDelta)?.revision).toBe(200);
	});

	it("sends If-None-Match for an unchanged cursor", async () => {
		const client = new QueryClient();
		const ok = (data: object, etag: string) => ({ data, response: { status: 200, headers: new Headers({ ETag: etag }) } });
		remoteGetMock.mockResolvedValueOnce(ok(FULL_A, 'W/"a"'));
		await refreshRemoteConversation(client, "s2", "h1");
		remoteGetMock.mockResolvedValueOnce(ok({ ...DELTA_B, revision: 100 }, 'W/"b"'));
		await refreshRemoteConversation(client, "s2", "h1");
		expect(remoteGetMock.mock.calls[1][1].headers).toBeUndefined();
		remoteGetMock.mockResolvedValueOnce({ data: undefined, response: { status: 304 } });
		await refreshRemoteConversation(client, "s2", "h1");
		expect(remoteGetMock.mock.calls[2][1].headers).toEqual({ "If-None-Match": 'W/"b"' });
	});

	it("ignores an older delta that arrives after a newer one", async () => {
		const client = await seeded(FULL_A);
		let release!: () => void;
		const late = new Promise<void>((resolve) => { release = resolve; });
		remoteGetMock.mockImplementationOnce(async () => { await late; return { data: wire({ ...DELTA_B, revision: 150, messages: [msg("m2", 3, "stale")], activities: [], turns: [turn("t1")] }), response: { status: 200 } }; });
		const slow = refreshRemoteConversation(client, "s1", "h1");
		remoteGetMock.mockResolvedValueOnce({ data: DELTA_B, response: { status: 200 } });
		await refreshRemoteConversation(client, "s1", "h1");
		release();
		await slow;
		expect(live(client)?.revision).toBe(200);
		expect(live(client)?.items.find((item) => item.id === "m2")).toMatchObject({ text: "ab" });
	});

	it("keeps a millisecond-epoch revision exact through the cursor", async () => {
		const revision = 1792497601234;
		const client = await seeded({ ...FULL_A, revision });
		remoteGetMock.mockResolvedValueOnce({ data: undefined, response: { status: 304 } });
		await refreshRemoteConversation(client, "s1", "h1");
		expect(Number.isSafeInteger(revision)).toBe(true);
		expect(remoteGetMock.mock.calls[1][1].params.query.sinceRevision).toBe(revision);
	});

	it("keeps the cache on 304", async () => {
		const client = await seeded(FULL_A);
		const before = live(client);
		remoteGetMock.mockResolvedValueOnce({ data: undefined, response: { status: 304 } });
		await refreshRemoteConversation(client, "s1", "h1");
		expect(live(client)).toBe(before);
	});

	it("refetches a full page when the host rejects the cursor", async () => {
		const client = await seeded(FULL_A);
		const rejection = { code: "CONVERSATION_CURSOR_AHEAD" };
		apiErrorCodeMock.mockReturnValue("CONVERSATION_CURSOR_AHEAD");
		remoteGetMock.mockResolvedValueOnce({ error: rejection, response: { status: 409 } });
		remoteGetMock.mockResolvedValueOnce({ data: FULL_B, response: { status: 200 } });
		await refreshRemoteConversation(client, "s1", "h1");
		expect(remoteGetMock.mock.calls[2][1].params.query.sinceRevision).toBeUndefined();
		expect(live(client)?.latestSequence).toBe(4);
	});

	it("falls back to a full page on a branch switch", async () => {
		const client = await seeded(FULL_A);
		remoteGetMock.mockResolvedValueOnce({ data: { ...DELTA_B, activeBranchId: "b2" }, response: { status: 200 } });
		remoteGetMock.mockResolvedValueOnce({ data: { ...FULL_B, activeBranchId: "b2" }, response: { status: 200 } });
		await refreshRemoteConversation(client, "s1", "h1");
		expect(remoteGetMock.mock.calls[2][1].params.query.sinceRevision).toBeUndefined();
		expect(live(client)?.activeBranchId).toBe("b2");
		expect(live(client)?.items.map((item) => item.id)).toEqual(["m1", "a1", "m2", "m3"]);
	});

	it("drops the entries of a turn the delta marks rolled back", async () => {
		const client = await seeded(FULL_A);
		remoteGetMock.mockResolvedValueOnce({
			data: wire({ latestSequence: 3, oldestSequence: 1, revision: 150, delta: true, turns: [turn("t1", { rolledBack: true })] }),
			response: { status: 200 },
		});
		await refreshRemoteConversation(client, "s1", "h1");
		expect(live(client)?.items).toEqual([]);
	});
});
