import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, test, vi } from "vitest";
import type { CoordinatorRoster, CoordinatorRosterEntry } from "../lib/coordinators";
import type { WorkspaceSession } from "../types/workspace";

const { navigate, rosterQuery } = vi.hoisted(() => ({
	navigate: vi.fn(),
	rosterQuery: {
		data: undefined as CoordinatorRoster | undefined,
		isLoading: false,
	},
}));

vi.mock("@tanstack/react-router", () => ({ useNavigate: () => navigate }));
vi.mock("../hooks/useCoordinatorRoster", () => ({ useCoordinatorRoster: () => rosterQuery }));

import { SessionOwnershipSection } from "./SessionOwnershipSection";

const entry = (overrides: Partial<CoordinatorRosterEntry> = {}): CoordinatorRosterEntry => ({
	coordinatorSessionId: "ao-c1",
	coordinatorScope: "auth",
	isTerminated: false,
	latestSummary: null,
	needsRecovery: false,
	workerCount: 2,
	workerLimit: 3,
	activeWorkerCount: 2,
	workerSessionIds: ["ao-w1", "ao-w2"],
	...overrides,
});

const roster = (overrides: Partial<CoordinatorRoster> = {}): CoordinatorRoster => ({
	projectId: "ao",
	rootSessionId: "ao-root",
	capacity: { maxCoordinators: 2, maxWorkersPerCoordinator: 3, maxWorkers: 6, activeCoordinators: 1, activeWorkers: 2 },
	coordinators: [entry()],
	rootWorkerSessionIds: [],
	...overrides,
});

const worker = (ownerSessionId: string | null) =>
	({ id: "ao-w1", workspaceId: "ao", kind: "worker", ownerSessionId }) as unknown as WorkspaceSession;
const coordinator = { id: "ao-c1", workspaceId: "ao", coordinatorScope: "auth" } as unknown as WorkspaceSession;

describe("SessionOwnershipSection", () => {
	beforeEach(() => {
		navigate.mockClear();
		rosterQuery.data = undefined;
		rosterQuery.isLoading = false;
	});

	test("names the coordinator and scope that own a worker", () => {
		rosterQuery.data = roster();
		render(<SessionOwnershipSection session={worker("ao-c1")} />);
		expect(screen.getByTestId("session-owner")).toHaveTextContent("Owner: Coordinator ao-c1 (auth)");
		expect(screen.queryByTestId("ownership-needs-recovery")).not.toBeInTheDocument();
	});

	test("tells a root-owned worker from an unassigned one", () => {
		rosterQuery.data = roster();
		const { rerender } = render(<SessionOwnershipSection session={worker(null)} />);
		expect(screen.getByTestId("session-owner")).toHaveTextContent("Root-owned (root ao-root)");

		rosterQuery.data = roster({ rootSessionId: null });
		rerender(<SessionOwnershipSection session={worker(null)} />);
		expect(screen.getByTestId("session-owner")).toHaveTextContent("Unassigned (no live root)");
	});

	test("flags a worker whose owning coordinator needs recovery", () => {
		rosterQuery.data = roster({ coordinators: [entry({ isTerminated: true, needsRecovery: true })] });
		render(<SessionOwnershipSection session={worker("ao-c1")} />);
		expect(screen.getByRole("alert")).toHaveTextContent("the owning coordinator is terminated");
	});

	test("lists a coordinator's workers, scope, and latest summary", async () => {
		rosterQuery.data = roster({
			coordinators: [
				entry({
					latestSummary: {
						coordinatorSessionId: "ao-c1",
						createdAt: "2026-10-20T10:00:00Z",
						deliveryAttempts: 0,
						deliveryState: "pending",
						escalation: true,
						escalationReason: "scope_decision",
						id: "csum_1",
						projectId: "ao",
						result: "waiting on schema",
						scope: "auth",
						state: "blocked",
						submissionKey: "k1",
						workerSessionIds: ["ao-w1"],
					},
				}),
			],
		});
		render(<SessionOwnershipSection session={coordinator} />);
		expect(screen.getByText("Workers (2)")).toBeInTheDocument();
		expect(screen.getByText("auth")).toBeInTheDocument();
		expect(screen.getAllByTestId("coordinator-worker-row").map((row) => row.textContent)).toEqual(["ao-w1", "ao-w2"]);
		expect(screen.getByTestId("coordinator-latest-summary")).toHaveTextContent("blocked · waiting on schema");
		expect(screen.getByTestId("coordinator-latest-summary")).toHaveTextContent("Escalation: scope_decision");

		await userEvent.click(screen.getByText("ao-w2"));
		expect(navigate).toHaveBeenCalledWith({
			to: "/projects/$projectId/sessions/$sessionId",
			params: { projectId: "ao", sessionId: "ao-w2" },
		});
	});

	test("flags a terminated coordinator that still owns live workers", () => {
		rosterQuery.data = roster({ coordinators: [entry({ isTerminated: true, needsRecovery: true })] });
		render(<SessionOwnershipSection session={coordinator} />);
		expect(screen.getByRole("alert")).toHaveTextContent("terminated but still owns live workers");
	});

	test("reports an unavailable roster", () => {
		render(<SessionOwnershipSection session={coordinator} />);
		expect(screen.getByRole("status")).toHaveTextContent("Workers are unavailable right now.");
	});
});
