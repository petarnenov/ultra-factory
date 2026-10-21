import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, fireEvent } from "@testing-library/react";
import { describe, expect, test, vi } from "vitest";
import type { ProjectConfig } from "../../lib/coordinators";

const hostIds = ["h_aaa", "h_bbb", "h_ccc"];
const rootId = "ultra-factory-h52b551-3";

const { roster } = vi.hoisted(() => ({ roster: { current: undefined as unknown } }));

vi.mock("../../hooks/useCoordinatorRoster", () => ({ useCoordinatorRoster: () => ({ data: roster.current, isLoading: false }) }));
vi.mock("../../hooks/useRemoteHosts", () => ({
	useRemoteHosts: () => ({ hosts: [{ hostId: "h_aaa", label: "IMac" }, { hostId: "h_bbb", label: "Linux" }, { hostId: "h_ccc", label: "Z600" }], refresh: async () => {} }),
}));
vi.mock("../../hooks/useWorkspaceQuery", () => {
	const workers = [1, 2, 3, 4, 5].map((n) => ({ id: `remote-w${n}`, kind: "worker", ownerSessionId: "ultra-factory-h52b551-3", provider: "claude-code", model: "opus", hostId: "h_aaa", status: "working" }));
	const remoteOrchestrators = [
		{ id: "ultra-factory-h6b66de-85", kind: "orchestrator", projectId: "ultra-factory", hostId: "h_aaa" },
		{ id: "geowealth-h6b66de-2", kind: "orchestrator", projectId: "geowealth", hostId: "h_aaa" },
	];
	const sessions = [...workers, ...remoteOrchestrators];
	return {
		workspaceQueryOptions: { queryKey: ["workspaces"], queryFn: async () => [{ id: "ultra-factory", name: "ultra-factory", sessions }] },
		useRemoteProjectQuery: () => ({ data: undefined }),
	};
});

import { CoordinatorsSettingsForm } from "./CoordinatorsSettingsForm";

const config = {
	coordinators: {
		enabled: true,
		maxCoordinators: 3,
		maxWorkersPerCoordinator: 3,
		maxWorkers: 9,
		allowedHosts: hostIds,
		coordinatorAgent: { mode: "" },
		workerAgent: { mode: "" },
		rootWorkerAgent: { mode: "" },
	},
} as unknown as ProjectConfig;

describe("coordinators section with the stored config (#502)", () => {
	test("renders the stored config with an empty roster", async () => {
		roster.current = {
			projectId: "ultra-factory",
			rootSessionId: rootId,
			capacity: { maxCoordinators: 3, maxWorkersPerCoordinator: 3, maxWorkers: 9, activeCoordinators: 0, activeWorkers: 5 },
			coordinators: [],
			rootWorkerSessionIds: [],
		};
		render(
			<QueryClientProvider client={new QueryClient()}>
				<CoordinatorsSettingsForm config={config} onSaved={() => {}} projectId="ultra-factory" projectName="ultra-factory" />
			</QueryClientProvider>,
		);
		expect(await screen.findByTestId("roster-capacity")).toHaveTextContent("0/3");
		fireEvent.click(screen.getByRole("button", { name: "Start coordinator" }));
		expect(await screen.findByRole("dialog")).toBeInTheDocument();
	});
});
