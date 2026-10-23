import { expect, type Page } from "@playwright/test";
import { agentReadiness } from "../../src/renderer/test/agent-readiness-fixtures";
import { installFakeAgent } from "./fake-bridge";

export const projectId = "chat-prompt-suggestion";
export const sessionId = "chat-prompt-suggestion-worker";
export const turnId = "completed-turn";
export const prompt = "Fix the reconnect test to handle either frame order, then rerun the package race tests.";

export async function openChat(page: Page, suggestionReady = Promise.resolve(), suggestedPrompt = prompt, navigate = true) {
	const sent: string[] = [];
	await page.emulateMedia({ reducedMotion: "reduce" });
	await installFakeAgent(page, {
		projectId,
		workers: [{ id: sessionId, provider: "codex", title: "Review reconnect test", mode: "chat" }],
	});
	await page.route("http://127.0.0.1:8080/api/v1/**", async (route) => {
		const pathname = new URL(route.request().url()).pathname;
		if (pathname === "/api/v1/agents/readiness" || pathname === "/api/v1/agents/readiness/ensure") {
			await route.fulfill({ json: { agents: [agentReadiness("codex", "Codex")] } });
		} else if (pathname === `/api/v1/projects/${projectId}`) {
			await route.fulfill({ json: { status: "ok", project: { id: projectId, agent: "codex", config: { worker: { agent: "codex" } } } } });
		} else if (pathname === `/api/v1/sessions/${sessionId}/conversation`) {
			const completedAt = "2026-10-12T06:00:00Z";
			await route.fulfill({
				json: {
					conversationId: "conversation-prompt-suggestion",
					sessionId,
					harness: "codex",
					mode: "chat",
					controller: "ready",
					latestSequence: 2,
					oldestSequence: 1,
					hasMoreBefore: false,
					turns: [{ id: turnId, state: "completed", requestedAt: completedAt, startedAt: completedAt, completedAt }],
					messages: [
						{ id: "user-message", turnId, sequence: 1, revision: 0, role: "user", origin: "human", text: "Review the reconnect test.", streaming: false, createdAt: completedAt },
						{ id: "assistant-message", turnId, sequence: 2, revision: 0, role: "assistant", origin: "provider", text: "The first frame can be an acknowledgment or a command. Handle either order, then rerun the race tests.", streaming: false, createdAt: completedAt },
					],
					activities: [],
					settings: {},
				},
			});
		} else if (pathname.endsWith("/conversation/prompt-suggestion")) {
			expect(route.request().postDataJSON()).toEqual({ turnId });
			await suggestionReady;
			await route.fulfill({ json: { turnId, prompt: suggestedPrompt } });
		} else if (pathname.endsWith("/conversation/messages") && route.request().method() === "POST") {
			sent.push(route.request().postDataJSON().text);
			await route.fulfill({ json: { turnId: "next-turn", state: "running", duplicate: false } });
		} else if (pathname.endsWith("/conversation/models")) {
			await route.fulfill({ json: { models: [], selected: {} } });
		} else if (pathname.endsWith("/conversation/skills")) {
			await route.fulfill({ json: { skills: [] } });
		} else if (pathname.endsWith("/workspace/files")) {
			await route.fulfill({ json: { files: [], truncated: false } });
		} else if (pathname.endsWith("/interface-transition")) {
			await route.fulfill({ json: { supported: true, targetMode: "tui" } });
		} else {
			await route.fulfill({ json: { status: "ok" } });
		}
	});
	if (navigate) await page.goto(`/#/projects/${projectId}/sessions/${sessionId}`);
	const composer = page.getByRole("combobox", { name: "Message the agent" });
	if (navigate) await expect(composer).toBeVisible({ timeout: 15_000 });
	return { composer, sent, suggestion: page.getByTestId("prompt-suggestion") };
}
