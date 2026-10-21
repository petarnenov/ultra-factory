import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, test, vi } from "vitest";
import type { CoordinatorRoster, ProjectConfig } from "../../lib/coordinators";

const { post } = vi.hoisted(() => ({ post: vi.fn() }));

vi.mock("../../lib/api-client", () => ({
	apiClient: { POST: post },
	apiErrorMessage: (error: unknown) => String(error),
}));
vi.mock("../../lib/host-clients", () => ({ clientForSessionHost: () => ({ POST: post }) }));
vi.mock("../../hooks/useRemoteHosts", () => ({
	useRemoteHosts: () => ({
		hosts: [{ hostId: "Linux", label: "Linux", url: "", status: "connected" }],
		refresh: async () => {},
	}),
}));

import { StartCoordinatorDialog } from "./StartCoordinatorDialog";

const roster: CoordinatorRoster = {
	projectId: "ao",
	rootSessionId: "ao-root",
	capacity: { maxCoordinators: 2, maxWorkersPerCoordinator: 3, maxWorkers: 6, activeCoordinators: 0, activeWorkers: 0 },
	coordinators: [],
	rootWorkerSessionIds: [],
};

const config: ProjectConfig = { coordinators: { enabled: true } };

const renderDialog = (onStarted = vi.fn()) =>
	render(
		<StartCoordinatorDialog
			config={config}
			onOpenChange={() => {}}
			onStarted={onStarted}
			open
			projectId="ao"
			roster={roster}
		/>,
	);

describe("StartCoordinatorDialog", () => {
	beforeEach(() => {
		post.mockReset();
	});

	test("keeps start disabled until the scope is set", async () => {
		renderDialog();
		const start = screen.getByRole("button", { name: "Start coordinator" });
		expect(start).toBeDisabled();
		await userEvent.type(screen.getByLabelText("Scope"), "auth module");
		expect(start).toBeEnabled();
	});

	test("refuses a scope over 200 characters", async () => {
		renderDialog();
		await userEvent.type(screen.getByLabelText("Scope"), "x".repeat(201));
		expect(screen.getByText("Scope must be 200 characters or fewer.")).toBeInTheDocument();
		expect(screen.getByRole("button", { name: "Start coordinator" })).toBeDisabled();
	});

	test("refuses a worker limit above the project's per-coordinator limit", async () => {
		renderDialog();
		await userEvent.type(screen.getByLabelText("Scope"), "docs");
		await userEvent.type(screen.getByLabelText("Worker limit"), "4");
		expect(screen.getByText("Enter a worker limit from 1 to the project's per-coordinator limit.")).toBeInTheDocument();
		expect(screen.getByRole("button", { name: "Start coordinator" })).toBeDisabled();
	});

	test("starts on behalf of the root and shows the audit summary", async () => {
		post.mockResolvedValue({ data: { session: { id: "ao-c9", projectId: "ao" } }, error: undefined });
		const onStarted = vi.fn();
		renderDialog(onStarted);
		await userEvent.type(screen.getByLabelText("Scope"), "docs");
		await userEvent.click(screen.getByRole("button", { name: "Start coordinator" }));

		await waitFor(() => expect(post).toHaveBeenCalledTimes(1));
		const body = post.mock.calls[0][1].body;
		expect(body).toMatchObject({ projectId: "ao", kind: "coordinator", scope: "docs", operatorInitiated: true, origin: "ui" });
		expect(body.parentSessionId).toBeUndefined();
		expect(await screen.findByText("Coordinator ao-c9 started.")).toBeInTheDocument();
		expect(screen.getByText(/Operator audit: origin UI, root ao-root, scope docs/)).toBeInTheDocument();
		expect(onStarted).toHaveBeenCalled();
	});

	test("shows a daemon refusal as a readable message", async () => {
		post.mockResolvedValue({
			data: undefined,
			error: { code: "COORDINATOR_HOST_SLOTS_FULL", message: "no slots" },
		});
		renderDialog();
		await userEvent.type(screen.getByLabelText("Scope"), "docs");
		await userEvent.click(screen.getByRole("button", { name: "Start coordinator" }));
		expect(await screen.findByText("All worker slots on that host are in use.")).toBeInTheDocument();
	});
});
