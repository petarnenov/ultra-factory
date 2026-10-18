import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { CompletedTask, CompletedTaskReport } from "../hooks/useCompletedTasks";
import { CompletedTasksView, type CompletedTaskSelection } from "./CompletedTasksView";

type GetCall = { path: string; params: { path: Record<string, string>; query?: Record<string, unknown> } };

const hosts = vi.hoisted(() => ({
	get: vi.fn(),
	failing: new Set<string>(),
}));

vi.mock("../lib/host-clients", () => ({
	clientForSessionHost: (hostId?: string) => {
		if (hostId && hosts.failing.has(hostId)) throw new Error(`Host ${hostId} is not connected`);
		return { GET: (path: string, init: { params: GetCall["params"] }) => hosts.get(hostId ?? "local", path, init.params) };
	},
	labelForHost: (hostId: string) => (hostId === "host-b" ? "Build box" : undefined),
}));

function task(overrides: Partial<CompletedTask>): CompletedTask {
	return {
		hostId: "machine-a",
		sessionId: "ao-1",
		projectId: "ao",
		title: "Add completed tasks",
		prs: [],
		outcome: "merged",
		createdAt: "2026-10-18T09:00:00Z",
		completedAt: "2026-10-18T10:00:00Z",
		elapsedSeconds: 3600,
		harness: "claude-code",
		model: "claude-opus-5-5",
		effort: "medium",
		tokens: { inputTokens: 1200, cachedInputTokens: 400, outputTokens: 300, completeness: "complete" },
		...overrides,
	};
}

function mount(refs: { hostId: string; projectId: string }[], selected: CompletedTaskSelection | null = null) {
	const onSelect = vi.fn();
	const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
	render(
		<QueryClientProvider client={client}>
			<CompletedTasksView refs={refs} selected={selected} onSelect={onSelect} />
		</QueryClientProvider>,
	);
	return { onSelect };
}

describe("CompletedTasksView", () => {
	beforeEach(() => {
		hosts.get.mockReset();
		hosts.failing.clear();
	});

	it("merges host-qualified projects, keeps equal session IDs distinct, and reports unreachable hosts", async () => {
		hosts.failing.add("host-c");
		hosts.get.mockImplementation(async (hostId: string) => ({
			data: {
				hostId,
				tasks:
					hostId === "local"
						? [task({ title: "Local task", completedAt: "2026-10-18T10:00:00Z" })]
						: [task({ title: "Remote task", completedAt: "2026-10-18T11:00:00Z", tokens: { inputTokens: null, cachedInputTokens: null, outputTokens: null, completeness: "unavailable" }, elapsedSeconds: null })],
			},
		}));
		const { onSelect } = mount([
			{ hostId: "local", projectId: "ao" },
			{ hostId: "host-b", projectId: "ao" },
			{ hostId: "host-c", projectId: "ao" },
		]);

		const rows = await screen.findAllByRole("button", { name: /Open report for/ });
		expect(rows.map((row) => row.textContent)).toEqual(["Remote task", "Local task"]);
		expect(screen.getByRole("status")).toHaveTextContent("Some hosts could not be reached");
		expect(screen.getByRole("status")).toHaveTextContent("host-c: Host host-c is not connected");
		const remoteRow = rows[0].closest("tr");
		expect(remoteRow).not.toBeNull();
		expect(within(remoteRow as HTMLElement).getAllByText("Unavailable").length).toBeGreaterThanOrEqual(3);
		expect(within(remoteRow as HTMLElement).queryByText("0")).toBeNull();
		expect(within(remoteRow as HTMLElement).getByText(/Build box/)).toBeInTheDocument();

		await userEvent.click(rows[0]);
		expect(onSelect).toHaveBeenCalledWith({ ref: { hostId: "host-b", projectId: "ao" }, sessionId: "ao-1" });
	});

	it("filters by outcome through the daemon query and shows empty and error states", async () => {
		hosts.get.mockResolvedValue({ data: { hostId: "machine-a", tasks: [] } });
		mount([{ hostId: "local", projectId: "ao" }]);
		expect(await screen.findByText("No completed tasks yet")).toBeInTheDocument();

		await userEvent.click(screen.getByRole("button", { name: "Failed" }));
		expect(await screen.findByText("No completed tasks with this outcome")).toBeInTheDocument();
		expect(hosts.get).toHaveBeenLastCalledWith("local", "/api/v1/projects/{projectId}/completed-tasks", {
			path: { projectId: "ao" },
			query: { outcome: "failed", limit: 50, cursor: undefined },
		});
	});

	it("shows a blocking error when every host fails", async () => {
		hosts.get.mockResolvedValue({ error: { error: { code: "PROJECT_NOT_FOUND", message: "Unknown project" } } });
		mount([{ hostId: "local", projectId: "ao" }]);
		expect(await screen.findByRole("alert")).toHaveTextContent("Completed tasks could not be loaded.");
	});

	it("loads further pages on request", async () => {
		hosts.get.mockImplementation(async (_host: string, _path: string, params: GetCall["params"]) => ({
			data: params.query?.cursor
				? { hostId: "machine-a", tasks: [task({ sessionId: "ao-2", title: "Older task", completedAt: "2026-10-17T10:00:00Z" })] }
				: { hostId: "machine-a", tasks: [task({})], nextCursor: "50" },
		}));
		mount([{ hostId: "local", projectId: "ao" }]);
		await userEvent.click(await screen.findByRole("button", { name: "Load more" }));
		expect(await screen.findByText("Older task")).toBeInTheDocument();
		expect(screen.queryByRole("button", { name: "Load more" })).toBeNull();
	});

	it("renders a report with stage totals, partial usage, and unrecorded configuration", async () => {
		const report: CompletedTaskReport = {
			...task({ outcome: "cancelled", tokens: { inputTokens: 900, cachedInputTokens: null, outputTokens: 90, completeness: "partial" } }),
			config: { completeness: "unavailable", segments: [] },
			runs: [{ bindingId: 1, harness: "claude-code", nativeRootId: "root-1", model: "claude-opus-5-5", eventCount: 3, inputTokens: 900, cachedInputTokens: null, outputTokens: 90, completeness: "partial" }],
			stages: {
				completeness: "complete",
				visits: [
					{ stage: "building", enteredAt: "2026-10-18T09:00:00Z", leftAt: "2026-10-18T09:20:00Z", durationSeconds: 1200 },
					{ stage: "validating", enteredAt: "2026-10-18T09:20:00Z", leftAt: "2026-10-18T09:30:00Z", durationSeconds: 600 },
					{ stage: "building", enteredAt: "2026-10-18T09:30:00Z", leftAt: "2026-10-18T09:35:00Z", durationSeconds: 300 },
				],
				totals: [
					{ stage: "building", seconds: 1500, visits: 2, hasUnfinished: false },
					{ stage: "validating", seconds: 600, visits: 1, hasUnfinished: false },
				],
			},
		};
		hosts.get.mockResolvedValue({ data: report });
		mount([{ hostId: "local", projectId: "ao" }], { ref: { hostId: "local", projectId: "ao" }, sessionId: "ao-1" });

		expect(await screen.findByRole("heading", { name: "Add completed tasks" })).toBeInTheDocument();
		expect(hosts.get).toHaveBeenCalledWith("local", "/api/v1/projects/{projectId}/completed-tasks/{sessionId}", { path: { projectId: "ao", sessionId: "ao-1" } });
		expect(screen.getByText("25m 00s · visits: 2")).toBeInTheDocument();
		expect(screen.getByText("10m 00s · visits: 1")).toBeInTheDocument();
		expect(screen.getAllByText("Not visited")).toHaveLength(2);
		expect(screen.getByText(/Configuration changes were not recorded/)).toHaveTextContent("claude-code · claude-opus-5-5 · medium");
		expect(screen.getAllByText("Partial").length).toBeGreaterThanOrEqual(2);
		expect(screen.getByText("Cancelled")).toBeInTheDocument();
		expect(screen.getByText("This machine")).toBeInTheDocument();
	});

	it("renders stage history as unavailable when no transitions were recorded", async () => {
		hosts.get.mockResolvedValue({
			data: {
				...task({}),
				config: { completeness: "complete", segments: [{ source: "session", harness: "claude-code", model: "claude-opus-5-5", effort: "medium", since: "2026-10-18T09:00:00Z" }] },
				runs: [],
				stages: { completeness: "unavailable", visits: [], totals: [] },
			},
		});
		mount([{ hostId: "local", projectId: "ao" }], { ref: { hostId: "local", projectId: "ao" }, sessionId: "ao-1" });
		expect(await screen.findByText("Not recorded for this task.")).toBeInTheDocument();
		expect(screen.queryByText("Not visited")).toBeNull();
		await waitFor(() => expect(screen.getByText("Task")).toBeInTheDocument());
	});
});
