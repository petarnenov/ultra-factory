import { expect, type Page } from "@playwright/test";
import { installFakeAgent } from "./fake-bridge";

export const retrySessionId = "chat-retry";
export const retryRefusal = "stop the current turn before retrying this one";
const now = "2026-08-24T12:00:00Z";

function snapshot(running: boolean) {
	return {
		conversationId: "conversation-chat-retry",
		sessionId: retrySessionId,
		harness: "codex",
		mode: "chat",
		controller: running ? "busy" : "ready",
		latestSequence: running ? 3 : 1,
		oldestSequence: 1,
		hasMoreBefore: false,
		turns: [
			{
				id: "turn-failed",
				state: "failed",
				providerTurnId: "provider-failed",
				requestedAt: now,
				startedAt: now,
				completedAt: now,
				errorMessage: "stream disconnected before completion",
			},
			...(running ? [{ id: "turn-running", state: "running", providerTurnId: "provider-running", requestedAt: now, startedAt: now }] : []),
		],
		messages: [
			{
				kind: "message",
				id: "user-failed",
				turnId: "turn-failed",
				sequence: 1,
				revision: 0,
				role: "user",
				origin: "human",
				text: "Fix the failing tests",
				streaming: false,
				createdAt: now,
			},
			...(running
				? [{ kind: "message", id: "user-running", turnId: "turn-running", sequence: 2, revision: 0, role: "user", origin: "human", text: "Check the work before retrying", streaming: false, createdAt: now }]
				: []),
		],
		activities: [],
		settings: {},
	};
}

export async function installRetryConversation(page: Page, running: boolean, navigate = true): Promise<void> {
	await installFakeAgent(page, {
		workers: [{ id: retrySessionId, title: "Retry evidence", mode: "chat" }],
	});
	await page.route(`**/api/v1/sessions/${retrySessionId}/**`, async (route) => {
		const path = new URL(route.request().url()).pathname;
		if (path.endsWith("/conversation") && route.request().method() === "GET") {
			await route.fulfill({ json: snapshot(running) });
			return;
		}
		if (path.endsWith("/turn-failed/retry")) {
			await route.fulfill({
				status: 409,
				json: { error: { code: "CHAT_RETRY_BUSY", message: retryRefusal } },
			});
			return;
		}
		if (path.endsWith("/conversation/models")) {
			await route.fulfill({ json: { models: [], selected: {} } });
			return;
		}
		if (path.endsWith("/conversation/skills")) {
			await route.fulfill({ json: { skills: [] } });
			return;
		}
		if (path.endsWith("/workspace/files")) {
			await route.fulfill({ json: { files: [], truncated: false } });
			return;
		}
		if (path.endsWith("/interface-transition")) {
			await route.fulfill({ json: { supported: true, targetMode: "tui" } });
			return;
		}
		await route.fulfill({ status: 404, json: { error: { code: "NOT_FOUND", message: "not found" } } });
	});
	if (navigate) {
		await page.goto(`/#/projects/fake-proj/sessions/${retrySessionId}`);
		await expect(page.getByRole("log", { name: "Conversation" })).toBeVisible();
	}
}
