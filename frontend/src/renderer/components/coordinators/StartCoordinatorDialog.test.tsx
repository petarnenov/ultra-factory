import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, test, vi } from "vitest";
import type { CoordinatorRoster, ProjectConfig } from "../../lib/coordinators";
import { COORDINATOR_BRIEF_TEMPLATE, firstUnfilledPlaceholder } from "./brief-template";

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

const config: ProjectConfig = { defaultBranch: "dev", coordinators: { enabled: true } };

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

/** Fills every template placeholder except the acceptance criteria. */
const fillBriefExceptAcceptance = async () => {
	await userEvent.type(screen.getByLabelText("Coordinator name"), "Ada");
	await userEvent.type(screen.getByLabelText("Scope"), "auth module");
	await userEvent.type(screen.getByLabelText("Out of scope"), "billing");
	await userEvent.type(screen.getByLabelText("Issue or task id"), "#503");
	await userEvent.type(screen.getByLabelText("Title"), "Start brief");
};

const fillBrief = async () => {
	await fillBriefExceptAcceptance();
	await userEvent.type(screen.getByLabelText("Acceptance criteria"), "placeholders are blocked");
};

const startButton = () => screen.getByRole("button", { name: "Start coordinator" });

describe("StartCoordinatorDialog", () => {
	beforeEach(() => {
		post.mockReset();
	});

	test("blocks start and names the first unfilled placeholder", async () => {
		renderDialog();
		expect(startButton()).toBeDisabled();
		expect(screen.getByText("Fill in <coordinator name> before starting. The brief still has a template placeholder.")).toBeInTheDocument();
		await userEvent.type(screen.getByLabelText("Coordinator name"), "Ada");
		expect(screen.getByText(/Fill in <one bounded area/)).toBeInTheDocument();
		expect(startButton()).toBeDisabled();
	});

	test("enables start once every placeholder is filled", async () => {
		renderDialog();
		await fillBrief();
		expect(startButton()).toBeEnabled();
		expect(screen.queryByText(/^Fill in /)).not.toBeInTheDocument();
	});

	test("keeps a missing acceptance criteria marker in the brief and blocks start", async () => {
		renderDialog();
		await fillBriefExceptAcceptance();
		expect(startButton()).toBeDisabled();
		expect(screen.getByText("Fill in <acceptance criteria: fill in> before starting. The brief still has a template placeholder.")).toBeInTheDocument();
	});

	test("a task title containing a placeholder still enables start", async () => {
		renderDialog();
		await userEvent.type(screen.getByLabelText("Coordinator name"), "Ada");
		await userEvent.type(screen.getByLabelText("Scope"), "auth module");
		await userEvent.type(screen.getByLabelText("Out of scope"), "billing");
		await userEvent.type(screen.getByLabelText("Issue or task id"), "#503");
		await userEvent.type(screen.getByLabelText("Title"), "Fix <title> handling");
		await userEvent.type(screen.getByLabelText("Acceptance criteria"), "no placeholder in the brief");
		expect(startButton()).toBeEnabled();
		expect(screen.queryByText(/^Fill in /)).not.toBeInTheDocument();
	});

	test("restores the raw template with Reset to template and blocks start", async () => {
		renderDialog();
		await fillBrief();
		expect(startButton()).toBeEnabled();
		await userEvent.click(screen.getByRole("button", { name: "Reset to template" }));
		expect(screen.getByLabelText<HTMLTextAreaElement>("Brief").value).toBe(COORDINATOR_BRIEF_TEMPLATE);
		expect(startButton()).toBeDisabled();
		expect(screen.getByText("Fill in <coordinator name> before starting. The brief still has a template placeholder.")).toBeInTheDocument();
	});

	test("refuses a scope over 200 characters", async () => {
		renderDialog();
		await userEvent.type(screen.getByLabelText("Scope"), "x".repeat(201));
		expect(screen.getByText("Scope must be 200 characters or fewer.")).toBeInTheDocument();
		expect(startButton()).toBeDisabled();
	});

	test("refuses a worker limit above the project's per-coordinator limit", async () => {
		renderDialog();
		await userEvent.type(screen.getByLabelText("Scope"), "docs");
		await userEvent.type(screen.getByLabelText("Worker limit"), "4");
		expect(screen.getByText("Enter a worker limit from 1 to the project's per-coordinator limit.")).toBeInTheDocument();
		expect(startButton()).toBeDisabled();
	});

	test("starts on behalf of the root with a brief that has no placeholder left", async () => {
		post.mockResolvedValue({ data: { session: { id: "ao-c9", projectId: "ao" } }, error: undefined });
		const onStarted = vi.fn();
		renderDialog(onStarted);
		await fillBrief();
		await userEvent.click(startButton());

		await waitFor(() => expect(post).toHaveBeenCalledTimes(1));
		const body = post.mock.calls[0][1].body;
		expect(body).toMatchObject({ projectId: "ao", kind: "coordinator", scope: "auth module", operatorInitiated: true, origin: "ui" });
		expect(body.parentSessionId).toBeUndefined();
		expect(firstUnfilledPlaceholder(body.prompt)).toBeNull();
		expect(body.prompt).toContain("Assignment for Ada.");
		expect(body.prompt).toContain("Scope: auth module");
		expect(body.prompt).toContain("Out of scope: billing");
		expect(body.prompt).toContain("Integration branch / PR base: dev");
		expect(body.prompt).toContain("Worker branch name: co-auth-module/<task-id>");
		expect(body.prompt).toContain("1. #503 - Start brief\n   Acceptance criteria: placeholders are blocked\n   Files likely touched: none listed");
		expect(await screen.findByText("Coordinator ao-c9 started.")).toBeInTheDocument();
		expect(screen.getByText(/Operator audit: origin UI, root ao-root, scope auth module/)).toBeInTheDocument();
		expect(onStarted).toHaveBeenCalled();
	});

	test("shows a daemon refusal as a readable message", async () => {
		post.mockResolvedValue({
			data: undefined,
			error: { code: "COORDINATOR_HOST_SLOTS_FULL", message: "no slots" },
		});
		renderDialog();
		await fillBrief();
		await userEvent.click(startButton());
		expect(await screen.findByText("All worker slots on that host are in use.")).toBeInTheDocument();
	});
});
