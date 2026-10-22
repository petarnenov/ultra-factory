import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, test, vi } from "vitest";
import type { CoordinatorRoster as RosterData, CoordinatorRosterEntry, ProjectConfig } from "../../lib/coordinators";

const { rosterQuery } = vi.hoisted(() => ({
	rosterQuery: { data: undefined as RosterData | undefined, isLoading: false },
}));

vi.mock("../../hooks/useCoordinatorRoster", () => ({ useCoordinatorRoster: () => rosterQuery }));
vi.mock("../../hooks/useRemoteHosts", () => ({ useRemoteHosts: () => ({ hosts: [], refresh: async () => {} }) }));
vi.mock("../../hooks/useWorkspaceQuery", () => ({
	workspaceQueryOptions: { queryKey: ["workspaces"], queryFn: async () => [] },
	useRemoteProjectQuery: () => ({ data: undefined }),
}));
vi.mock("./StartCoordinatorDialog", () => ({ StartCoordinatorDialog: () => null }));

import { CoordinatorRoster } from "./CoordinatorRoster";

const entry = (overrides: Partial<CoordinatorRosterEntry> = {}): CoordinatorRosterEntry => ({
	coordinatorSessionId: "ao-c1",
	coordinatorScope: "auth",
	agentProfileId: null,
	workers: [],
	isTerminated: false,
	latestSummary: null,
	needsRecovery: false,
	workerCount: 0,
	workerLimit: 2,
	effectiveWorkerLimit: 2,
	effectivePlacementLimit: 2,
	hostSlots: [{ hostId: "Linux", slots: 1, used: 0 }],
	activeWorkerCount: 0,
	workerSessionIds: [],
	...overrides,
});

const roster = (overrides: Partial<RosterData> = {}): RosterData => ({
	projectId: "ao",
	rootSessionId: "ao-root",
	capacity: { maxCoordinators: 2, maxWorkersPerCoordinator: 3, maxWorkers: 6, activeCoordinators: 1, activeWorkers: 0, staleWorkers: 0 },
	coordinators: [entry()],
	rootWorkerSessionIds: [],
	rootWorkers: [],
	...overrides,
});

const enabled: ProjectConfig = { coordinators: { enabled: true } };

const renderRoster = (config: ProjectConfig = enabled) =>
	render(
		<QueryClientProvider client={new QueryClient()}>
			<CoordinatorRoster config={config} projectId="ao" />
		</QueryClientProvider>,
	);

describe("CoordinatorRoster", () => {
	beforeEach(() => {
		rosterQuery.data = undefined;
		rosterQuery.isLoading = false;
	});

	test("shows the root, the coordinator limit and the host slot usage", () => {
		rosterQuery.data = roster();
		renderRoster();
		expect(screen.getByText(/ao-root/)).toBeInTheDocument();
		expect(screen.getByText("auth")).toBeInTheDocument();
		expect(screen.getByTestId("coordinator-limit")).toHaveTextContent("0/2");
		expect(screen.getByText("Linux 0/1")).toBeInTheDocument();
	});

	test("lists each worker with its profile", () => {
		rosterQuery.data = roster({
			coordinators: [
				entry({
					activeWorkerCount: 1,
					workerSessionIds: ["ao-w1"],
					workers: [{ sessionId: "ao-w1", agentProfileId: "fast", remote: false }],
				}),
			],
		});
		renderRoster();
		expect(screen.getByText("ao-w1")).toBeInTheDocument();
		expect(screen.getByText("fast")).toBeInTheDocument();
	});

	test("shows remote owned workers beside their host label", () => {
		rosterQuery.data = roster({
			coordinators: [entry({
				workerCount: 1,
				activeWorkerCount: 1,
				workers: [{ sessionId: "shared-worker", agentProfileId: null, remote: true, hostId: "host-build", hostLabel: "Air builder" }],
			})],
		});
		renderRoster();
		expect(screen.getByTestId("coordinator-remote-worker-row")).toHaveTextContent("shared-worker on Air builder");
	});

	test("lists root-owned remote workers with their host label", () => {
		rosterQuery.data = roster({
			rootWorkers: [{ sessionId: "root-worker", agentProfileId: null, remote: true, hostId: "host-root", hostLabel: "Air root" }],
		});
		renderRoster();
		expect(screen.getByTestId("root-worker-list")).toHaveTextContent("root-worker on Air root");
	});

	test("disables start with the reason when coordinators are off", () => {
		rosterQuery.data = roster();
		renderRoster({ coordinators: { enabled: false } });
		expect(screen.getByRole("button", { name: "Start coordinator" })).toBeDisabled();
		expect(screen.getByText("Enable coordinators to start one.")).toBeInTheDocument();
	});

	test("disables start when there is no live root", () => {
		rosterQuery.data = roster({ rootSessionId: null });
		renderRoster();
		expect(screen.getByRole("button", { name: "Start coordinator" })).toBeDisabled();
		expect(screen.getByText("No live root. Start the orchestrator first.")).toBeInTheDocument();
	});

	test("disables start when the coordinator cap is reached", () => {
		rosterQuery.data = roster({
			capacity: { maxCoordinators: 1, maxWorkersPerCoordinator: 3, maxWorkers: 6, activeCoordinators: 1, activeWorkers: 0, staleWorkers: 0 },
		});
		renderRoster();
		expect(screen.getByRole("button", { name: "Start coordinator" })).toBeDisabled();
		expect(screen.getByText("The coordinator limit is reached.")).toBeInTheDocument();
	});
});
