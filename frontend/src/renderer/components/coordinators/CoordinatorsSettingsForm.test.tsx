import { act, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, test, vi } from "vitest";

const { put } = vi.hoisted(() => ({ put: vi.fn() }));

vi.mock("../../lib/api-client", () => ({
	apiClient: { PUT: put },
	apiErrorMessage: (error: unknown) => String(error),
}));
vi.mock("../../lib/host-clients", () => ({ clientForHost: () => ({ PUT: put }) }));
vi.mock("../../hooks/useRemoteHosts", () => ({
	useRemoteHosts: () => ({
		hosts: [{ hostId: "linux-1", label: "Linux", url: "", status: "connected" }],
		refresh: async () => {},
	}),
}));
vi.mock("./CoordinatorRoster", () => ({ CoordinatorRoster: () => null }));

import type { ProjectConfig } from "../../lib/coordinators";
import { CoordinatorsSettingsForm } from "./CoordinatorsSettingsForm";

const config: ProjectConfig = {
	defaultBranch: "dev",
	sessionPrefix: "ao",
	autoReview: true,
	terminateOnPrMerge: false,
	coordinators: { enabled: false, maxWorkers: 4 },
};

describe("CoordinatorsSettingsForm", () => {
	beforeEach(() => {
		put.mockReset();
		put.mockResolvedValue({ error: undefined });
	});

	test("saves the coordinators block and keeps every other project field", async () => {
		const onSaved = vi.fn();
		render(<CoordinatorsSettingsForm config={config} onSaved={onSaved} projectId="ao" projectName="Agent Orchestrator" />);

		await userEvent.click(screen.getByRole("switch", { name: "Enable coordinators" }));
		await userEvent.click(screen.getByRole("button", { name: "Save coordinators" }));

		await waitFor(() => expect(put).toHaveBeenCalledTimes(1));
		const body = put.mock.calls[0][1].body;
		expect(body.displayName).toBe("Agent Orchestrator");
		expect(body.config.defaultBranch).toBe("dev");
		expect(body.config.sessionPrefix).toBe("ao");
		expect(body.config.autoReview).toBe(true);
		expect(body.config.terminateOnPrMerge).toBe(false);
		expect(body.config.coordinators).toMatchObject({ enabled: true, maxWorkers: 4 });
		await waitFor(() => expect(onSaved).toHaveBeenCalled());
	});

	test("blocks saving a limit above the hard maximum", async () => {
		render(<CoordinatorsSettingsForm config={config} onSaved={() => {}} projectId="ao" projectName="ao" />);
		const field = screen.getByLabelText("Coordinators");
		await userEvent.clear(field);
		await userEvent.type(field, "9");
		expect(screen.getByRole("button", { name: "Save coordinators" })).toBeDisabled();
		expect(screen.getByText("Enter a whole number from 0 to the hard maximum, or leave it blank for the default.")).toBeInTheDocument();
		expect(put).not.toHaveBeenCalled();
	});

	test("shows the local-workers warning only when the switch is on", async () => {
		render(<CoordinatorsSettingsForm config={config} onSaved={() => {}} projectId="ao" projectName="ao" />);
		expect(screen.queryByText("Coordinator workers then run on the root's machine, so implementation work runs there.")).not.toBeInTheDocument();
		await userEvent.click(screen.getByRole("switch", { name: "Allow local workers" }));
		expect(screen.getByText("Coordinator workers then run on the root's machine, so implementation work runs there.")).toBeInTheDocument();
	});
});

describe("CoordinatorsSettingsForm save protocol", () => {
	beforeEach(() => {
		put.mockReset();
		put.mockResolvedValue({ error: undefined });
	});

	const submitDialogForm = () =>
		act(() => {
			(document.getElementById("project-settings-form") as HTMLFormElement).requestSubmit();
		});

	test("a successful save resets the baseline, so the next close is clean", async () => {
		const onSaveState = vi.fn();
		render(
			<CoordinatorsSettingsForm config={config} onSaveState={onSaveState} onSaved={() => {}} projectId="ao" projectName="ao" />,
		);
		await userEvent.click(screen.getByRole("switch", { name: "Enable coordinators" }));
		expect(onSaveState).toHaveBeenLastCalledWith(expect.objectContaining({ dirty: true }));

		await userEvent.click(screen.getByRole("button", { name: "Save coordinators" }));
		await waitFor(() => expect(onSaveState).toHaveBeenLastCalledWith({ phase: "saved", dirty: false, error: undefined }));

		submitDialogForm();
		expect(put).toHaveBeenCalledTimes(1);
	});

	test("a section switch or close saves a dirty draft before it proceeds", async () => {
		const onSaved = vi.fn();
		render(<CoordinatorsSettingsForm config={config} onSaved={onSaved} projectId="ao" projectName="ao" />);
		await userEvent.click(screen.getByRole("switch", { name: "Enable coordinators" }));

		submitDialogForm();

		await waitFor(() => expect(put).toHaveBeenCalledTimes(1));
		expect(put.mock.calls[0][1].body.config.coordinators.enabled).toBe(true);
		await waitFor(() => expect(onSaved).toHaveBeenCalled());
	});

	test("a draft with an unknown profile is held, not dropped, and says why", async () => {
		const onSaveState = vi.fn();
		const stale: ProjectConfig = {
			...config,
			coordinators: { agentProfiles: [{ id: "fast", harness: "codex", model: "", effort: "" }], coordinatorAgent: { mode: "fixed", profileId: "gone" } },
		};
		render(<CoordinatorsSettingsForm config={stale} onSaveState={onSaveState} onSaved={() => {}} projectId="ao" projectName="ao" />);
		await userEvent.click(screen.getByRole("switch", { name: "Enable coordinators" }));

		submitDialogForm();

		expect(put).not.toHaveBeenCalled();
		expect(onSaveState).toHaveBeenLastCalledWith({
			phase: "failed",
			dirty: true,
			error: "Fix the highlighted fields before saving.",
		});
		expect(screen.getByText("This selection uses a profile that no longer exists. Pick another.")).toBeInTheDocument();
	});
});
