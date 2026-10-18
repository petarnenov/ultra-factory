import { render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { WorkerReportsPanel } from "./WorkerReportsPanel";

const reports = vi.hoisted(() => ({ data: [] as unknown[], isLoading: false, error: null as unknown }));
vi.mock("../../hooks/useWorkerReports", () => ({ useWorkerReports: () => reports }));
vi.mock("../../lib/use-session-link-navigation", () => ({ useSessionLinkNavigation: () => vi.fn() }));

describe("WorkerReportsPanel", () => {
	beforeEach(() => {
		reports.data = [];
		reports.isLoading = false;
		reports.error = null;
	});

	it("shows source host, agent, session, statuses, links, and chronological entries", () => {
			reports.data = [
			{ id: "remote-1", projectId: "p", sourceProjectId: "remote-project", sessionId: "worker", hostId: "host-a", hostLabel: "Build box", agent: "codex", sessionName: "Compile", state: "needs_input", note: "Choose a target.", outputs: [{ kind: "pr_created", reference: "https://github.com/o/r/pull/1", label: "PR 1" }, { kind: "artifact", reference: "result.md", label: "Result" }], createdAt: "2026-10-18T10:00:00Z", repeatCount: 1 },
			{ id: "remote-1", projectId: "p", sessionId: "worker", hostId: "host-b", hostLabel: "Test box", agent: "claude-code", state: "done", note: "Finished.", outputs: [], createdAt: "2026-10-18T10:01:00Z", repeatCount: 1 },
		] as never[];
		render(<WorkerReportsPanel projectId="p" />);
		expect(screen.getByRole("complementary", { name: "Reports" })).toBeInTheDocument();
		expect(screen.getByText("Build box (host-a) · codex · Compile")).toBeInTheDocument();
		expect(screen.getByText("Test box (host-b) · claude-code · worker")).toBeInTheDocument();
		expect(screen.getByText("Needs input")).toBeInTheDocument();
		expect(screen.getByText("Done")).toBeInTheDocument();
		expect(screen.getByRole("link", { name: "PR 1" })).toHaveAttribute("href", "https://github.com/o/r/pull/1");
		expect(screen.getByRole("link", { name: "Result" })).toHaveAttribute("href", "ao://sessions/remote-project/worker/artifacts/result.md");
	});

	it("shows an honest local machine fallback, a session link, and long report text", () => {
		const longNote = "detail ".repeat(200);
		reports.data = [{ id: "local-1", projectId: "p", sessionId: "worker", state: "stuck", note: longNote, outputs: [], createdAt: "2026-10-18T10:00:00Z", repeatCount: 1 }] as never[];
		render(<WorkerReportsPanel projectId="p" />);
		expect(screen.getByText(/This machine · Agent unavailable · worker/)).toBeInTheDocument();
		expect(screen.getByRole("link", { name: "worker" })).toHaveAttribute("href", "ao://sessions/p/worker");
		expect(screen.getByText(/detail detail detail/)).toBeInTheDocument();
	});

	it("shows an empty state", () => {
		render(<WorkerReportsPanel projectId="p" />);
		expect(screen.getByText("No worker reports yet.")).toBeInTheDocument();
	});
});
