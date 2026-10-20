import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, expect, it, vi } from "vitest";
import { AggregateProjectBoard } from "./AggregateProjectBoard";

const mocks = vi.hoisted(() => ({
	navigate: vi.fn(),
	local: [] as Array<Record<string, unknown>>,
	remote: [] as Array<Record<string, unknown>>,
	associations: [] as Array<{ hostId: string; projectId: string }>,
	hosts: [] as Array<{ hostId: string; label: string; status: "connected" | "offline" | "connecting" }>,
	failed: [] as string[],
	loaded: [] as string[],
}));

vi.mock("@tanstack/react-router", async (importOriginal) => ({
	...await importOriginal<typeof import("@tanstack/react-router")>(),
	useNavigate: () => mocks.navigate,
}));
vi.mock("../hooks/useWorkspaceQuery", () => ({
	useWorkspaceQuery: () => ({ data: mocks.local, isPending: false, isError: false }),
	useProjectIdentity: () => ({ data: { aggregateProjects: mocks.associations }, isPending: false, isError: false }),
	useRemoteWorkspaces: () => ({
		data: mocks.remote,
		loadedProjectHostIds: mocks.loaded,
		loadedSessionHostIds: mocks.loaded,
		failedHostIds: mocks.failed,
	}),
}));
vi.mock("../hooks/useRemoteHosts", () => ({ useRemoteHosts: () => ({ hosts: mocks.hosts }) }));
vi.mock("../lib/host-clients", () => ({ labelForHost: (hostId: string) => `Host ${hostId}` }));
vi.mock("./SessionsBoardAdapters", () => ({
	sessionsBoardLabels: () => ({ columnAria: (label: string) => `${label} sessions` }),
	BoardSessionCardAdapter: ({ session, sourceLabel, onOpen }: { session: { id: string; title: string }; sourceLabel: string; onOpen: () => void }) => <button type="button" aria-label={session.title} onClick={onOpen}>{sourceLabel}: {session.title}</button>,
}));

function session(id: string, title: string, workspaceId: string) {
	return { id, title, workspaceId, hostId: undefined, kind: "worker", provider: "codex", status: "working", kanbanColumn: "building", isTerminated: false, updatedAt: "2026-10-18T10:00:00Z", prs: [] };
}

beforeEach(() => {
	mocks.navigate.mockReset();
	mocks.local = [{ id: "local-project", name: "Project", sessions: [session("same-id", "Local worker", "local-project")] }];
	mocks.remote = [{ hostId: "host-a", id: "remote-project", name: "Different display name", sessions: [session("same-id", "Remote worker", "remote-project")] }];
	mocks.associations = [{ hostId: "host-a", projectId: "remote-project" }];
	mocks.hosts = [{ hostId: "host-a", label: "Linux", status: "connected" }];
	mocks.failed = [];
	mocks.loaded = ["host-a"];
});

it("keeps equal session IDs distinct and opens the host-qualified project session", () => {
	render(<AggregateProjectBoard projectId="local-project" />);
	expect(screen.getByRole("button", { name: "Local worker" })).toBeInTheDocument();
	expect(screen.getByRole("button", { name: "Remote worker" })).toBeInTheDocument();
	fireEvent.click(screen.getByRole("button", { name: "Remote worker" }));
	expect(mocks.navigate).toHaveBeenCalledWith({
		to: "/host/$hostId/project/$projectId/session/$sessionId",
		params: { hostId: "host-a", projectId: "remote-project", sessionId: "same-id" },
	});
});

it("marks a configured offline project partial and keeps local workers visible", () => {
	mocks.hosts = [{ hostId: "host-a", label: "Linux", status: "offline" }];
	mocks.remote = [];
	render(<AggregateProjectBoard projectId="local-project" />);
	expect(screen.getByRole("status")).toHaveTextContent("Some hosts could not be reached");
	expect(screen.getByText(/remote-project/)).toBeInTheDocument();
	expect(screen.getByRole("button", { name: "Local worker" })).toBeInTheDocument();
});

it("does not show a complete-empty message when all remote results are unavailable", () => {
	mocks.local = [{ id: "local-project", name: "Project", sessions: [] }];
	mocks.hosts = [{ hostId: "host-a", label: "Linux", status: "offline" }];
	mocks.remote = [];
	render(<AggregateProjectBoard projectId="local-project" />);
	expect(screen.getByText(/Unavailable project state is unknown/)).toBeInTheDocument();
});
