import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, test, vi } from "vitest";
import type { CoordinatorRoster as RosterData, ProjectConfig } from "../../lib/coordinators";

const hostIds = ["h_imac", "h_linux", "h_z600"];
const rootId = "ultra-factory-h52b551-3";

const { rosterQuery } = vi.hoisted(() => ({
	rosterQuery: { data: undefined as RosterData | undefined, isLoading: false },
}));

vi.mock("../../hooks/useCoordinatorRoster", () => ({ useCoordinatorRoster: () => rosterQuery }));
vi.mock("../../hooks/useRemoteHosts", () => ({
	useRemoteHosts: () => ({
		hosts: [
			{ hostId: "h_imac", label: "IMac" },
			{ hostId: "h_linux", label: "Linux" },
			{ hostId: "h_z600", label: "Z600" },
		],
		refresh: async () => {},
	}),
}));
// The reported crash came with remote orchestrators of two projects on the
// paired host, so the local session list carries both kinds of session.
vi.mock("../../hooks/useWorkspaceQuery", () => ({
	workspaceQueryOptions: {
		queryKey: ["workspaces"],
		queryFn: async () => [
			{
				id: "ultra-factory",
				name: "ultra-factory",
				sessions: ["w1", "w2", "w3", "w4", "w5"].map((id) => ({
					id: `remote-${id}`,
					kind: "worker",
					ownerSessionId: rootId,
					provider: "claude-code",
					model: "opus",
					status: "working",
				})),
			},
		],
	},
	useRemoteProjectQuery: () => ({ data: undefined }),
}));

import { CoordinatorsSettingsForm } from "./CoordinatorsSettingsForm";

// The daemon's stored block from the report: three empty modes, three allowed
// hosts, limits 3/3/9 (#502).
const storedConfig = {
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

const emptyRoster: RosterData = {
	projectId: "ultra-factory",
	rootSessionId: rootId,
	capacity: { maxCoordinators: 3, maxWorkersPerCoordinator: 3, maxWorkers: 9, activeCoordinators: 0, activeWorkers: 5, staleWorkers: 0 },
	coordinators: [],
	rootWorkerSessionIds: [],
	rootWorkers: Array.from({ length: 5 }, (_, index) => ({ sessionId: `remote-w${index + 1}`, agentProfileId: null, remote: true, hostId: hostIds[index % hostIds.length], hostLabel: hostIds[index % hostIds.length] })),
};

const renderSection = (config: ProjectConfig = storedConfig) =>
	render(
		<QueryClientProvider client={new QueryClient()}>
			<CoordinatorsSettingsForm config={config} onSaved={() => {}} projectId="ultra-factory" projectName="ultra-factory" />
		</QueryClientProvider>,
	);

describe("Coordinators settings with the stored config (#502)", () => {
	beforeEach(() => {
		rosterQuery.data = undefined;
		rosterQuery.isLoading = false;
	});

	test("renders the capacity line and Start with an empty roster", () => {
		rosterQuery.data = emptyRoster;
		renderSection();
		expect(screen.getByTestId("roster-capacity")).toHaveTextContent("Coordinators 0/3");
		expect(screen.getByTestId("roster-capacity")).toHaveTextContent("Workers 5/9");
		expect(screen.getByText("Root workers: 5")).toBeInTheDocument();
		expect(screen.getByRole("button", { name: "Start coordinator" })).toBeEnabled();
		expect(screen.queryByRole("alert")).not.toBeInTheDocument();
	});

	test("reads the three empty modes as inherit instead of throwing", () => {
		rosterQuery.data = emptyRoster;
		renderSection();
		for (const label of ["Coordinator agent", "Worker agent", "Root workers agent"]) {
			expect(screen.getByLabelText(label)).toHaveTextContent("Inherit");
		}
	});

	test("opens the start dialog without a render failure", async () => {
		rosterQuery.data = emptyRoster;
		renderSection();
		fireEvent.click(screen.getByRole("button", { name: "Start coordinator" }));
		expect(await screen.findByRole("dialog")).toBeInTheDocument();
	});
});
