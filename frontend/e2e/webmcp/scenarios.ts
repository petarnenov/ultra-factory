import { expect, type Page } from "@playwright/test";
import type { KanbanColumn } from "@aoagents/product-ui";
import type { UpdateState } from "../../src/main/update-settings";
import type { GlobalSettingsSection } from "../../src/renderer/stores/ui-store";
import { agentReadiness } from "../../src/renderer/test/agent-readiness-fixtures";
import type { ToolErrorCode } from "../../src/renderer/webmcp/model-context";
import { installFakeAgent, installFakeBridge } from "../support/fake-bridge";

// ── Step grammar ─────────────────────────────────────────────────────────────

// What the original specs capture in closures (`sent` via `page.route`).
export type HostState = { sent: string[] };

export type ToolStep = {
	kind: "tool";
	name: string;
	args?: Record<string, unknown>;
	// Passes only when the tool fails with this ToolError code.
	rejects?: ToolErrorCode;
};
// A test-bridge push; every driver runs it once, outside the timed window.
export type BridgeStep = { kind: "bridge"; run: (page: Page) => Promise<void>; label: string };
// Deep-equals `path` (see `valueAtPath`) in the `read` tool's result.
export type ReadExpectStep = { kind: "expect"; read: string; path: string; equals: unknown };
export type HostExpectStep = { kind: "expect-host"; label: string; check: (host: HostState) => void };
export type ExpectStep = ReadExpectStep | HostExpectStep;
export type Step = ToolStep | BridgeStep | ExpectStep;

export type PhaseAction = ToolStep & { id: string };

// Runs bridge (untimed), actions, then checks before the next phase. Actions
// own their args: one tool can take different args in different phases.
export type Phase = { id: string; bridge?: BridgeStep; actions: PhaseAction[]; checks: ExpectStep[] };

export type JevAction = { id: string; phase: string; tool: string; args: Record<string, unknown> };

// totalMs covers actions and assertions; only bridge pushes are excluded.
export type PlaywrightTiming = { totalMs: number; bridgeMs: number; assertMs: number };

export type Scenario = {
	id: string;
	page: "board" | "settings" | "chat";
	route: string;
	source: { file: string; title: string };
	// Runs before `goto(route)`: fake bridge, fake agent, route stubs.
	setup: (page: Page, host: HostState) => Promise<void>;
	phases: Phase[];
	// `phases` flattened in execution order, for the scripted driver.
	tools: Step[];
	// Untimed readiness: the original spec's first visibility check.
	playwrightReady: (page: Page, host: HostState) => Promise<void>;
	// The original spec's steps with the same locators and expects.
	playwright: (page: Page, host: HostState) => Promise<PlaywrightTiming>;
	// Phase actions with ids, so a chooser never keys args by tool name.
	jevActions: JevAction[];
	jevGoal: string;
};

export function createHostState(): HostState {
	return { sent: [] };
}

// ── Tool contracts ───────────────────────────────────────────────────────────

// WM-1.2.x page tool arguments. Switch to the exported types once the
// tool modules land under src/renderer/webmcp/tools/.
type NoArgs = Record<string, never>;
type ChatWaitPredicate = "suggestion_visible" | "suggestion_hidden" | "composer_equals" | "retry_error_visible" | "message_sent";
type ToolArgs = {
	"board.wait_for_card": { sessionId: string; column: KanbanColumn; timeoutMs?: number };
	"settings.select_section": { id: GlobalSettingsSection };
	"settings.update.download": NoArgs;
	"settings.wait_for_update_state": { state: UpdateState; timeoutMs?: number };
	"chat.type": { text: string };
	"chat.accept_suggestion": NoArgs;
	"chat.dismiss_suggestion": NoArgs;
	"chat.send": NoArgs;
	"chat.retry": { turnId: string };
	"chat.wait_for": { where: ChatWaitPredicate; value?: string; timeoutMs?: number };
};
type ReadTool = "board.read" | "settings.read" | "chat.read";

function action<N extends keyof ToolArgs>(id: string, name: N, args: ToolArgs[N]): PhaseAction {
	return { kind: "tool", id, name, args };
}

function rejecting(step: PhaseAction, code: ToolErrorCode): PhaseAction {
	return { ...step, rejects: code };
}

function readExpect(read: ReadTool, path: string, equals: unknown): ReadExpectStep {
	return { kind: "expect", read, path, equals };
}

function sentEquals(messages: string[]): HostExpectStep {
	return { kind: "expect-host", label: `sent equals ${JSON.stringify(messages)}`, check: (host) => expect(host.sent).toEqual(messages) };
}

function bridge(label: string, run: (page: Page) => Promise<void>): BridgeStep {
	return { kind: "bridge", label, run };
}

function defineScenario(definition: Omit<Scenario, "tools" | "jevActions">): Scenario {
	return {
		...definition,
		tools: definition.phases.flatMap((phase) => [...(phase.bridge ? [phase.bridge] : []), ...phase.actions, ...phase.checks]),
		jevActions: definition.phases.flatMap((phase) =>
			phase.actions.map((step) => ({ id: step.id, phase: phase.id, tool: step.name, args: step.args ?? {} })),
		),
	};
}

// Times a Playwright body from construction, minus its bridge pushes.
class PlaywrightClock {
	private readonly startedAt = performance.now();
	private bridgeMs = 0;
	private assertMs = 0;

	async bridge(page: Page, step: BridgeStep): Promise<void> {
		const start = performance.now();
		await step.run(page);
		this.bridgeMs += performance.now() - start;
	}

	async assert(check: () => Promise<unknown>): Promise<void> {
		const start = performance.now();
		await check();
		this.assertMs += performance.now() - start;
	}

	timing(): PlaywrightTiming {
		const totalMs = performance.now() - this.startedAt - this.bridgeMs;
		return { totalMs, bridgeMs: this.bridgeMs, assertMs: this.assertMs };
	}
}

// ── Board ────────────────────────────────────────────────────────────────────

const boardColumns: KanbanColumn[] = ["building", "validating", "needs_review", "ready"];
const fakeProjectBoard = "/#/projects/fake-proj";

const columnCard = (page: Page, column: KanbanColumn, id: string) =>
	page.locator(`[data-testid="board-column"][data-column="${column}"] [data-session-id="${id}"]`);
const boardCard = (page: Page, id: string) => page.locator(`[data-testid="board-session-card"][data-session-id="${id}"]`);
const cardIdsIn = (column: KanbanColumn) => `columns.[id=${column}].cards.*.id`;

async function boardVisible(page: Page): Promise<void> {
	await expect(page.getByTestId("board")).toBeVisible();
}

const boardColumnsScenario = defineScenario({
	id: "board.columns",
	page: "board",
	route: "/#/projects/ao-demo",
	source: { file: "e2e/smoke-t0.spec.ts", title: "renderer: board renders all status columns @T0 @BRD" },
	setup: async () => {},
	phases: [{ id: "columns", actions: [], checks: [readExpect("board.read", "columns.*.id", boardColumns)] }],
	playwrightReady: boardVisible,
	playwright: async (page) => {
		const clock = new PlaywrightClock();
		const columns = page.getByTestId("board-column");
		await clock.assert(async () => {
			await expect(columns).toHaveCount(4);
			await expect(columns.nth(0)).toContainText("Building");
			await expect(columns.nth(1)).toContainText("Validating");
			await expect(columns.nth(2)).toContainText("In review");
			await expect(columns.nth(3)).toContainText("Ready");
		});
		return clock.timing();
	},
	jevGoal: "Confirm the board shows the building, validating, needs_review and ready columns in that order.",
});

const moverNeedsInput = bridge('__aoFakeAgent.setStatus("mover", "needs_input", "waiting_input")', async (page) => {
	await page.evaluate(() => window.__aoFakeAgent!.setStatus("mover", "needs_input", "waiting_input"));
});

const cardNeedsReviewScenario = defineScenario({
	id: "board.card-needs-review",
	page: "board",
	route: fakeProjectBoard,
	source: { file: "e2e/smoke-board.spec.ts", title: "renderer: card moves columns when its status changes @T0 @BRD" },
	setup: async (page) => {
		await installFakeAgent(page, { workers: [{ id: "mover", title: "Wandering worker", status: "working" }] });
	},
	phases: [
		{
			id: "building",
			actions: [],
			checks: [readExpect("board.read", cardIdsIn("building"), ["mover"]), readExpect("board.read", cardIdsIn("needs_review"), [])],
		},
		{
			id: "needs-review",
			bridge: moverNeedsInput,
			actions: [action("wait-needs-review", "board.wait_for_card", { sessionId: "mover", column: "needs_review" })],
			checks: [
				readExpect("board.read", cardIdsIn("building"), []),
				readExpect("board.read", "columns.[id=needs_review].cards.[id=mover].attention", true),
			],
		},
	],
	playwrightReady: boardVisible,
	playwright: async (page) => {
		const clock = new PlaywrightClock();
		await clock.assert(async () => {
			await expect(columnCard(page, "building", "mover")).toBeVisible();
			await expect(columnCard(page, "needs_review", "mover")).toHaveCount(0);
		});
		await clock.bridge(page, moverNeedsInput);
		await clock.assert(async () => {
			await expect(columnCard(page, "needs_review", "mover")).toBeVisible();
			await expect(columnCard(page, "building", "mover")).toHaveCount(0);
			await expect(columnCard(page, "needs_review", "mover")).toContainText("Input needed");
		});
		return clock.timing();
	},
	jevGoal: "Wait for the mover card to move from building to needs_review once it asks for input.",
});

const liveMergeable = bridge('__aoFakeAgent.setStatus("live", "mergeable", "idle")', async (page) => {
	await page.evaluate(() => window.__aoFakeAgent!.setStatus("live", "mergeable", "idle"));
});

const cardReadyScenario = defineScenario({
	id: "board.card-ready",
	page: "board",
	route: fakeProjectBoard,
	source: { file: "e2e/smoke-board.spec.ts", title: "renderer: SSE pushes card updates without a manual refresh @T0 @BRD" },
	setup: async (page) => {
		await installFakeAgent(page, { workers: [{ id: "live", title: "Live worker", status: "working" }] });
	},
	phases: [
		{ id: "building", actions: [], checks: [readExpect("board.read", cardIdsIn("building"), ["live"])] },
		{
			id: "ready",
			bridge: liveMergeable,
			actions: [action("wait-ready", "board.wait_for_card", { sessionId: "live", column: "ready" })],
			checks: [readExpect("board.read", cardIdsIn("ready"), ["live"])],
		},
	],
	playwrightReady: boardVisible,
	playwright: async (page) => {
		const clock = new PlaywrightClock();
		await clock.assert(() => expect(columnCard(page, "building", "live")).toContainText("Working"));
		await clock.bridge(page, liveMergeable);
		await clock.assert(async () => {
			await expect(columnCard(page, "ready", "live")).toBeVisible();
			await expect(columnCard(page, "ready", "live")).toContainText("Ready");
		});
		return clock.timing();
	},
	jevGoal: "Wait for the live card to reach the ready column after it becomes mergeable.",
});

const spawnWorker = bridge('__aoFakeAgent.createWorker({ id: "fake-spawn", ... })', async (page) => {
	await page.evaluate(() =>
		window.__aoFakeAgent!.createWorker({ id: "fake-spawn", title: "Spawning worker", activity: "exited" }),
	);
});

const newCardScenario = defineScenario({
	id: "board.new-card",
	page: "board",
	route: fakeProjectBoard,
	source: { file: "e2e/smoke-sessions.spec.ts", title: "renderer: new session card appears in the spawning/working state @T0 @SES" },
	setup: async (page) => {
		await installFakeAgent(page);
	},
	phases: [
		{ id: "empty", actions: [], checks: [readExpect("board.read", "columns.*.cards", boardColumns.map(() => []))] },
		{
			id: "spawned",
			bridge: spawnWorker,
			actions: [action("wait-building", "board.wait_for_card", { sessionId: "fake-spawn", column: "building" })],
			checks: [readExpect("board.read", cardIdsIn("building"), ["fake-spawn"])],
		},
	],
	playwrightReady: boardVisible,
	playwright: async (page) => {
		const clock = new PlaywrightClock();
		await clock.assert(() => expect(boardCard(page, "fake-spawn")).toHaveCount(0));
		await clock.bridge(page, spawnWorker);
		await clock.assert(async () => {
			await expect(columnCard(page, "building", "fake-spawn")).toBeVisible();
			await expect(boardCard(page, "fake-spawn")).toContainText("Working");
		});
		return clock.timing();
	},
	jevGoal: "Wait for the newly spawned worker card to appear in the building column.",
});

// ── Settings ─────────────────────────────────────────────────────────────────

const settingsRoute = "/#/settings";

const settingsSectionsScenario = defineScenario({
	id: "settings.sections",
	page: "settings",
	route: settingsRoute,
	source: { file: "e2e/smoke-t0.spec.ts", title: "renderer: global settings page renders all sections @T0 @SET" },
	setup: async () => {},
	phases: [
		{
			id: "updates",
			actions: [action("select-updates", "settings.select_section", { id: "updates" })],
			checks: [readExpect("settings.read", "activeSection", "updates")],
		},
		{
			id: "help",
			actions: [action("select-help", "settings.select_section", { id: "help" })],
			checks: [readExpect("settings.read", "activeSection", "help")],
		},
	],
	playwrightReady: async (page) => {
		await expect(page.getByTestId("settings-page")).toBeVisible();
	},
	playwright: async (page) => {
		const clock = new PlaywrightClock();
		await page.getByRole("button", { name: "Updates" }).click();
		await clock.assert(() => expect(page.locator('[data-testid="settings-section"][data-section="updates"]')).toBeVisible());
		await page.getByRole("button", { name: "Help" }).click();
		await clock.assert(() => expect(page.getByLabel("Title")).toBeVisible());
		return clock.timing();
	},
	jevGoal: "Open the updates settings section, then the help section.",
});

async function updatesButtonVisible(page: Page): Promise<void> {
	await expect(page.getByRole("button", { name: "Updates", exact: true })).toBeVisible();
}

const downloadedVersion = "0.12.8-nightly.202608241447";
const nightlyAppVersion = "0.12.7-nightly.202608240525";

const updateDownloadedScenario = defineScenario({
	id: "settings.update-downloaded",
	page: "settings",
	route: settingsRoute,
	source: { file: "e2e/updates-settings.spec.ts", title: "downloaded update keeps the full version readable and actions aligned" },
	setup: async (page) => {
		await installFakeBridge(page, {
			version: nightlyAppVersion,
			updateSettings: { enabled: true, channel: "nightly", nightlyAck: true, feature: null },
			updateStatus: { state: "downloaded", version: downloadedVersion, checkedAt: new Date("2026-08-24T17:11:00.000Z").getTime() },
		});
	},
	phases: [
		{
			id: "downloaded",
			actions: [action("select-updates", "settings.select_section", { id: "updates" })],
			checks: [
				readExpect("settings.read", "updates.state", "downloaded"),
				readExpect("settings.read", "updates.version", downloadedVersion),
				readExpect("settings.read", "updates.appVersion", nightlyAppVersion),
			],
		},
	],
	playwrightReady: updatesButtonVisible,
	// Viewport, color scheme and the line-count and bounding-box geometry
	// checks are layout assertions with no tool equivalent and are excluded.
	playwright: async (page) => {
		const clock = new PlaywrightClock();
		await page.getByRole("button", { name: "Updates" }).click();
		await clock.assert(async () => {
			await expect(page.getByTestId("update-status-line")).toContainText("Ready to install.");
			const version = page.getByTestId("app-version");
			await expect(version).toHaveText("v0.12.7");
			await expect(version).toHaveAttribute("aria-label", `Current version: v${nightlyAppVersion}`);
			await expect(page.getByText(nightlyAppVersion, { exact: true })).toBeVisible();
			await expect(page.getByRole("button", { name: "Install Update" })).toBeVisible();
			await expect(page.getByRole("button", { name: "Check for updates" })).toBeVisible();
			await expect(page.getByRole("switch", { name: "Automatic updates" })).toBeChecked();
			await expect(page.getByRole("button", { name: "Channel", exact: true })).toContainText("Nightly");
			await expect(page.locator(".nightly-warning")).toBeVisible();
		});
		return clock.timing();
	},
	jevGoal: "Open the updates section and confirm the downloaded nightly update is ready to install.",
});

const pushDownloading = bridge('__aoFakeUpdates.setStatus({ state: "downloading", percent: 42 })', async (page) => {
	await page.evaluate(() => {
		window.__aoFakeUpdates!.setStatus({ state: "downloading", version: "2.0.0", percent: 42, transferred: 42_000_000, total: 100_000_000 });
	});
});
const pushPreparing = bridge('__aoFakeUpdates.setStatus({ state: "preparing" })', async (page) => {
	await page.evaluate(() => {
		window.__aoFakeUpdates!.setStatus({ state: "preparing", version: "2.0.0", percent: 100, staged: { version: "2.0.0", stagedAt: 10, escalated: false, ready: false } });
	});
});
const pushDownloaded = bridge('__aoFakeUpdates.setStatus({ state: "downloaded" })', async (page) => {
	await page.evaluate(() => {
		window.__aoFakeUpdates!.setStatus({ state: "downloaded", version: "2.0.0" });
	});
});

const updateLiveFlowScenario = defineScenario({
	id: "settings.update-live-flow",
	page: "settings",
	route: settingsRoute,
	source: { file: "e2e/updates-settings.spec.ts", title: "@P0 live update flow stays synchronized without reopening Settings" },
	setup: async (page) => {
		await installFakeBridge(page, {
			version: "1.0.0",
			updateStatus: { state: "available", version: "2.0.0" },
			updateSettings: { enabled: false, channel: "latest", nightlyAck: false, feature: null },
		});
	},
	phases: [
		{
			id: "start-download",
			actions: [
				action("select-updates", "settings.select_section", { id: "updates" }),
				action("download", "settings.update.download", {}),
				action("wait-downloading", "settings.wait_for_update_state", { state: "downloading" }),
			],
			checks: [readExpect("settings.read", "updates.state", "downloading")],
		},
		{
			id: "progress",
			bridge: pushDownloading,
			actions: [],
			checks: [readExpect("settings.read", "updates.percent", 42), readExpect("settings.read", "updates.appVersion", "1.0.0")],
		},
		{
			id: "preparing",
			bridge: pushPreparing,
			actions: [action("wait-preparing", "settings.wait_for_update_state", { state: "preparing" })],
			checks: [readExpect("settings.read", "updates.state", "preparing")],
		},
		{
			id: "downloaded",
			bridge: pushDownloaded,
			actions: [action("wait-downloaded", "settings.wait_for_update_state", { state: "downloaded" })],
			checks: [readExpect("settings.read", "updates.state", "downloaded"), readExpect("settings.read", "updates.version", "2.0.0")],
		},
	],
	playwrightReady: updatesButtonVisible,
	playwright: async (page) => {
		const clock = new PlaywrightClock();
		await page.getByRole("button", { name: "Updates", exact: true }).click();
		await page.getByRole("button", { name: "Update to v2.0.0" }).click();
		await clock.assert(async () => {
			await expect(page.getByTestId("sidebar-update-downloading")).toContainText(/Starting download…|Downloading… 0%/);
			await expect(page.getByTestId("update-status-line")).toContainText(/Starting download…|Downloading…/);
		});
		await clock.bridge(page, pushDownloading);
		await clock.assert(async () => {
			await expect(page.getByTestId("sidebar-update-downloading")).toContainText("Downloading… 42%");
			await expect(page.getByTestId("update-status-line")).toContainText("42.0 / 100.0 MB");
			await expect(page.getByTestId("app-version")).toHaveText("v1.0.0");
			await expect(page.getByText("Updating to v2.0.0")).toBeVisible();
		});
		await clock.bridge(page, pushPreparing);
		await clock.assert(async () => {
			await expect(page.getByTestId("sidebar-update-ready")).toContainText("Restart to update");
			await expect(page.getByTestId("update-status-line")).toContainText("Preparing update…");
			await expect(page.getByRole("button", { name: "Install Update", exact: true })).toHaveCount(0);
		});
		await clock.bridge(page, pushDownloaded);
		await clock.assert(async () => {
			await expect(page.getByTestId("sidebar-update-ready")).toContainText("Restart to update");
			await expect(page.getByTestId("sidebar-update-ready")).toHaveAttribute("aria-label", "Restart to install update v2.0.0");
			await expect(page.getByTestId("sidebar-update-ready")).not.toHaveClass(/text-success/);
			await expect(page.getByRole("button", { name: "Restart & install", exact: true })).toBeEnabled();
		});
		return clock.timing();
	},
	jevGoal: "Download the available 2.0.0 update and follow it through downloading and preparing until it is downloaded.",
});

// ── Chat ─────────────────────────────────────────────────────────────────────

// Mirrors openChat in chat-prompt-suggestion.spec.ts without its goto, so
// the driver navigates. Move to e2e/support once the WM-1.2.3 extraction lands.
const suggestionProjectId = "chat-prompt-suggestion";
const suggestionSessionId = "chat-prompt-suggestion-worker";
const suggestionTurnId = "completed-turn";
const suggestedPrompt = "Fix the reconnect test to handle either frame order, then rerun the package race tests.";
const editedPrompt = `${suggestedPrompt} Include the fix in this PR.`;

async function installSuggestionChat(page: Page, host: HostState): Promise<void> {
	await page.emulateMedia({ reducedMotion: "reduce" });
	await installFakeAgent(page, {
		projectId: suggestionProjectId,
		workers: [{ id: suggestionSessionId, provider: "codex", title: "Review reconnect test", mode: "chat" }],
	});
	await page.route("http://127.0.0.1:8080/api/v1/**", async (route) => {
		const pathname = new URL(route.request().url()).pathname;
		if (pathname === "/api/v1/agents/readiness" || pathname === "/api/v1/agents/readiness/ensure") {
			await route.fulfill({ json: { agents: [agentReadiness("codex", "Codex")] } });
		} else if (pathname === `/api/v1/projects/${suggestionProjectId}`) {
			await route.fulfill({ json: { status: "ok", project: { id: suggestionProjectId, agent: "codex", config: { worker: { agent: "codex" } } } } });
		} else if (pathname === `/api/v1/sessions/${suggestionSessionId}/conversation`) {
			await route.fulfill({ json: suggestionConversation() });
		} else if (pathname.endsWith("/conversation/prompt-suggestion")) {
			expect(route.request().postDataJSON()).toEqual({ turnId: suggestionTurnId });
			await route.fulfill({ json: { turnId: suggestionTurnId, prompt: suggestedPrompt } });
		} else if (pathname.endsWith("/conversation/messages") && route.request().method() === "POST") {
			host.sent.push(route.request().postDataJSON().text);
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
}

function suggestionConversation() {
	const completedAt = "2026-10-12T06:00:00Z";
	const turnId = suggestionTurnId;
	return {
		conversationId: "conversation-prompt-suggestion",
		sessionId: suggestionSessionId,
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
	};
}

const suggestionRoute = `/#/projects/${suggestionProjectId}/sessions/${suggestionSessionId}`;
const suggestionSource = "e2e/chat-prompt-suggestion.spec.ts";
const chatComposer = (page: Page) => page.getByRole("combobox", { name: "Message the agent" });
const promptSuggestion = (page: Page) => page.getByTestId("prompt-suggestion");

async function composerVisible(page: Page): Promise<void> {
	await expect(chatComposer(page)).toBeVisible({ timeout: 15_000 });
}

const suggestionShown: Phase = {
	id: "suggestion-shown",
	actions: [action("wait-suggestion", "chat.wait_for", { where: "suggestion_visible" })],
	checks: [readExpect("chat.read", "composer.suggestion", suggestedPrompt), readExpect("chat.read", "composer.text", "")],
};

const suggestionAcceptScenario = defineScenario({
	id: "chat.suggestion-accept",
	page: "chat",
	route: suggestionRoute,
	source: { file: suggestionSource, title: "Tab inserts the suggested next message for editing, and Enter sends the edited draft @T0" },
	setup: installSuggestionChat,
	phases: [
		suggestionShown,
		{
			id: "send-empty",
			actions: [rejecting(action("send-empty", "chat.send", {}), "precondition")],
			checks: [readExpect("chat.read", "composer.text", ""), readExpect("chat.read", "composer.suggestion", suggestedPrompt), sentEquals([])],
		},
		{
			id: "accept",
			actions: [action("accept", "chat.accept_suggestion", {})],
			checks: [readExpect("chat.read", "composer.text", suggestedPrompt), readExpect("chat.read", "composer.suggestion", null), sentEquals([])],
		},
		{
			id: "send-edited",
			actions: [action("type-edited", "chat.type", { text: editedPrompt }), action("send-edited", "chat.send", {})],
			checks: [sentEquals([editedPrompt])],
		},
	],
	playwrightReady: composerVisible,
	// The ghost-text color comparison and the screenshot are visual checks
	// with no tool equivalent and are excluded.
	playwright: async (page, host) => {
		const clock = new PlaywrightClock();
		const composer = chatComposer(page);
		const suggestion = promptSuggestion(page);
		await clock.assert(async () => {
			await expect(suggestion).toHaveText(suggestedPrompt);
			await expect(composer).toHaveText("");
		});
		await composer.press("Enter");
		await clock.assert(async () => {
			await expect(composer).toHaveText("");
			await expect(suggestion).toHaveText(suggestedPrompt);
			expect(host.sent).toEqual([]);
		});
		await composer.press("Tab");
		await clock.assert(async () => {
			await expect(composer).toHaveText(suggestedPrompt);
			await expect(composer).toBeFocused();
			await expect(suggestion).toBeHidden();
			expect(host.sent).toEqual([]);
		});
		await page.keyboard.type(" Include the fix in this PR.");
		await composer.press("Enter");
		await clock.assert(() => expect.poll(() => host.sent).toEqual([editedPrompt]));
		return clock.timing();
	},
	jevGoal: "Accept the prompt suggestion, extend it with the PR note, and send it.",
});

const suggestionDismissScenario = defineScenario({
	id: "chat.suggestion-dismiss",
	page: "chat",
	route: suggestionRoute,
	source: { file: suggestionSource, title: "Escape dismisses the suggestion and restores normal Tab focus navigation @T0" },
	setup: installSuggestionChat,
	phases: [
		suggestionShown,
		{
			id: "dismissed",
			actions: [action("dismiss", "chat.dismiss_suggestion", {})],
			checks: [readExpect("chat.read", "composer.suggestion", null), readExpect("chat.read", "composer.text", ""), sentEquals([])],
		},
	],
	playwrightReady: composerVisible,
	playwright: async (page, host) => {
		const clock = new PlaywrightClock();
		const composer = chatComposer(page);
		const suggestion = promptSuggestion(page);
		await clock.assert(() => expect(suggestion).toHaveText(suggestedPrompt));
		await composer.press("Escape");
		await clock.assert(() => expect(suggestion).toBeHidden());
		await composer.press("Tab");
		await clock.assert(async () => {
			await expect(composer).not.toBeFocused();
			await expect(composer).toHaveText("");
			expect(host.sent).toEqual([]);
		});
		return clock.timing();
	},
	jevGoal: "Dismiss the prompt suggestion without sending anything.",
});

// Mirrors installRetryConversation(page, false) in chat-retry.spec.ts without
// its goto. Move to e2e/support once that helper is extracted.
const retrySessionId = "chat-retry";
const retryRefusal = "stop the current turn before retrying this one";

async function installRefusedRetry(page: Page): Promise<void> {
	await installFakeAgent(page, { workers: [{ id: retrySessionId, title: "Retry evidence", mode: "chat" }] });
	await page.route(`**/api/v1/sessions/${retrySessionId}/**`, async (route) => {
		const path = new URL(route.request().url()).pathname;
		if (path.endsWith("/conversation") && route.request().method() === "GET") {
			await route.fulfill({ json: failedTurnConversation() });
		} else if (path.endsWith("/turn-failed/retry")) {
			await route.fulfill({ status: 409, json: { error: { code: "CHAT_RETRY_BUSY", message: retryRefusal } } });
		} else if (path.endsWith("/conversation/models")) {
			await route.fulfill({ json: { models: [], selected: {} } });
		} else if (path.endsWith("/conversation/skills")) {
			await route.fulfill({ json: { skills: [] } });
		} else if (path.endsWith("/workspace/files")) {
			await route.fulfill({ json: { files: [], truncated: false } });
		} else if (path.endsWith("/interface-transition")) {
			await route.fulfill({ json: { supported: true, targetMode: "tui" } });
		} else {
			await route.fulfill({ status: 404, json: { error: { code: "NOT_FOUND", message: "not found" } } });
		}
	});
}

function failedTurnConversation() {
	const now = "2026-08-24T12:00:00Z";
	return {
		conversationId: "conversation-chat-retry",
		sessionId: retrySessionId,
		harness: "codex",
		mode: "chat",
		controller: "ready",
		latestSequence: 1,
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
		],
		activities: [],
		settings: {},
	};
}

const retryRefusedScenario = defineScenario({
	id: "chat.retry-refused",
	page: "chat",
	route: `/#/projects/fake-proj/sessions/${retrySessionId}`,
	source: { file: "e2e/chat-retry.spec.ts", title: "retry refusal is visible beside the failed turn @T0" },
	setup: installRefusedRetry,
	phases: [
		{
			id: "refused",
			actions: [
				action("retry", "chat.retry", { turnId: "turn-failed" }),
				action("wait-refusal", "chat.wait_for", { where: "retry_error_visible", value: retryRefusal }),
			],
			checks: [readExpect("chat.read", "turns.[id=turn-failed].retryError", retryRefusal)],
		},
	],
	playwrightReady: async (page) => {
		await expect(page.getByRole("log", { name: "Conversation" })).toBeVisible();
	},
	playwright: async (page) => {
		const clock = new PlaywrightClock();
		await page.getByRole("button", { name: "Retry this turn" }).click();
		await clock.assert(() => expect(page.getByText(retryRefusal)).toContainText(retryRefusal));
		return clock.timing();
	},
	jevGoal: "Retry the failed turn and confirm the refusal message is shown beside it.",
});

// ── Catalog ──────────────────────────────────────────────────────────────────

export const scenarios: Scenario[] = [
	boardColumnsScenario,
	cardNeedsReviewScenario,
	cardReadyScenario,
	newCardScenario,
	settingsSectionsScenario,
	updateDownloadedScenario,
	updateLiveFlowScenario,
	suggestionAcceptScenario,
	suggestionDismissScenario,
	retryRefusedScenario,
];
