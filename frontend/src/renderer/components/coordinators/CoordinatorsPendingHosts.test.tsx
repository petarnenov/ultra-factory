import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, afterEach, describe, expect, test, vi } from "vitest";
import type { CoordinatorRoster as RosterData, ProjectConfig } from "../../lib/coordinators";

const hostIds = ["h_imac", "h_linux", "h_z600"];
const rootId = "ultra-factory-h52b551-3";

const { rosterQuery } = vi.hoisted(() => ({
	rosterQuery: { data: undefined as RosterData | undefined, isLoading: false },
}));

vi.mock("../../hooks/useCoordinatorRoster", () => ({ useCoordinatorRoster: () => rosterQuery }));
const { remotes } = vi.hoisted(() => ({ remotes: {
	list: vi.fn(async () => ["h_imac", "h_linux", "h_z600"].map((hostId) => ({ hostId, label: hostId, url: `http://${hostId}:3011` }))),
	connect: vi.fn(() => new Promise(() => {})),
	disconnect: vi.fn(async () => {}),
} }));
vi.mock("../../lib/bridge", () => ({ aoBridge: { remotes } }));
vi.mock("../../lib/cloud-session", async () => {
	const { useEffect, useState } = await import("react");
	return { useCloudSession: () => {
		const [ready, setReady] = useState(false);
		useEffect(() => setReady(true), []);
		return { status: ready ? "authenticated" : "loading", session: ready ? { user: { id: "test-account" } } : null };
	} };
});
vi.mock("../../hooks/useSettings", () => ({ useSettings: () => ({ settings: { cloudControlPlaneUrl: "" } }) }));
vi.mock("../../lib/telemetry", () => ({ captureRendererException: vi.fn(async () => {}) }));
// Local session metadata does not depend on remote host probes.
vi.mock("../../hooks/useWorkspaceQuery", () => ({
	workspaceQueryOptions: {
		queryKey: ["workspaces"],
		queryFn: async () => [
			{
				id: "ultra-factory",
				name: "ultra-factory",
				sessions: [],
			},
		],
	},
	useRemoteProjectQuery: () => ({ data: undefined }),
}));

import { CoordinatorsSettingsForm } from "./CoordinatorsSettingsForm";
import { useUiStore } from "../../stores/ui-store";
import { connectHost, connectedHosts, disconnectHost } from "../../lib/host-clients";

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
};

const renderSection = (config: ProjectConfig = storedConfig) =>
	render(
		<QueryClientProvider client={new QueryClient()}>
			<CoordinatorsSettingsForm config={config} onSaved={() => {}} projectId="ultra-factory" projectName="ultra-factory" />
		</QueryClientProvider>,
	);

describe("Local Coordinators while remote probes are pending", () => {
	beforeEach(async () => {
		remotes.connect.mockResolvedValueOnce({ hostId: "h_imac", label: "IMac", url: "http://h_imac:3011", base: "http://127.0.0.1:4000" });
		await connectHost("http://h_imac:3011", "h_imac");
		useUiStore.setState({ developerMode: true, remoteHosts: true });
		remotes.connect.mockClear();
		rosterQuery.data = emptyRoster;
		rosterQuery.isLoading = false;
	});
	afterEach(async () => act(async () => {
		useUiStore.setState({ developerMode: false, remoteHosts: false });
		await Promise.all(connectedHosts().map(disconnectHost));
	}));

	test("renders local capacity and opens an editable Start dialog before probes finish", async () => {
		renderSection();
		await waitFor(() => expect(remotes.connect).toHaveBeenCalled());
		expect(screen.getByTestId("roster-capacity")).toHaveTextContent("Coordinators 0/3");
		expect(screen.getByTestId("roster-capacity")).toHaveTextContent("Workers 5/9");
		expect(connectedHosts()).toEqual(["h_imac"]);
		await act(async () => { fireEvent.click(screen.getByRole("button", { name: "Start coordinator" })); });
		expect(screen.getByRole("dialog")).toBeInTheDocument();
		const name = screen.getByLabelText("Coordinator name");
		fireEvent.change(name, { target: { value: "Local work" } });
		expect(name).toHaveValue("Local work");
		expect(connectedHosts()).toEqual(["h_imac"]);
		expect(screen.queryByText("Could not render Coordinators")).not.toBeInTheDocument();
	});
});
