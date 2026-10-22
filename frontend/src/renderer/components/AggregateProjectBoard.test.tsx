import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, expect, it, vi } from "vitest";
import { useUiStore } from "../stores/ui-store";
import { AggregateProjectBoard } from "./AggregateProjectBoard";

const mocks = vi.hoisted(() => ({
	navigate: vi.fn(),
	local: [] as Array<Record<string, unknown>>,
	remote: [] as Array<Record<string, unknown>>,
	associations: [] as Array<Record<string, unknown>>,
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
	useProjectIdentity: () => ({ data: { aggregateEffective: mocks.associations }, isPending: false, isError: false }),
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
	useUiStore.setState({ developerMode: true, remoteHosts: true });
	mocks.navigate.mockReset();
	mocks.local = [{ id: "local-project", name: "Project", sessions: [session("same-id", "Local worker", "local-project")] }];
	mocks.remote = [{ hostId: "host-a", id: "remote-project", name: "Different display name", sessions: [session("same-id", "Remote worker", "remote-project")] }];
	mocks.associations = [{ hostId: "host-a", hostLabel: "Linux", projectId: "remote-project", source: "discovered", matchedBy: "repo", stale: false }];
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
	expect(screen.getAllByText(/remote-project/).length).toBeGreaterThan(0);
	expect(screen.getByRole("button", { name: "Local worker" })).toBeInTheDocument();
});

it.each([
	["offline", [], "the host is not connected"],
	["connected", ["host-a"], "the project was not found on the host"],
] as const)("names the reason for a %s host", (status, loaded, reason) => {
	mocks.hosts = [{ hostId: "host-a", label: "Linux", status }];
	mocks.loaded = [...loaded];
	mocks.remote = [];
	render(<AggregateProjectBoard projectId="local-project" />);
	expect(screen.getByRole("status")).toHaveTextContent(reason);
});

it("does not show a complete-empty message when all remote results are unavailable", () => {
	mocks.local = [{ id: "local-project", name: "Project", sessions: [] }];
	mocks.hosts = [{ hostId: "host-a", label: "Linux", status: "offline" }];
	mocks.remote = [];
	render(<AggregateProjectBoard projectId="local-project" />);
	expect(screen.getByText(/Unavailable project state is unknown/)).toBeInTheDocument();
});

it("stays loading while a connected host has not answered yet", () => {
	mocks.loaded = [];
	render(<AggregateProjectBoard projectId="local-project" />);
	expect(screen.getByText("Loading project boards…")).toBeInTheDocument();
});

it("shows local cards and a partial banner once a connected host has failed", () => {
	mocks.loaded = [];
	mocks.failed = ["host-a"];
	mocks.remote = [];
	render(<AggregateProjectBoard projectId="local-project" />);
	expect(screen.queryByText("Loading project boards…")).not.toBeInTheDocument();
	expect(screen.getByRole("status")).toHaveTextContent("Some hosts could not be reached");
	expect(screen.getByRole("status")).toHaveTextContent("the host failed or timed out");
	expect(screen.getByRole("button", { name: "Local worker" })).toBeInTheDocument();
});

it("renders partial, not loading, when the remote layer is gated off but links exist", () => {
	useUiStore.setState({ developerMode: false, remoteHosts: false });
	mocks.hosts = [];
	mocks.loaded = [];
	mocks.remote = [];
	render(<AggregateProjectBoard projectId="local-project" />);
	expect(screen.queryByText("Loading project boards…")).not.toBeInTheDocument();
	expect(screen.getByRole("status")).toHaveTextContent("Some hosts could not be reached");
	expect(screen.getAllByText(/remote-project/).length).toBeGreaterThan(0);
	expect(screen.getByRole("status")).toHaveTextContent("remote hosts are turned off in this app");
	expect(screen.getByRole("button", { name: "Local worker" })).toBeInTheDocument();
});

it("badges each aggregated host as discovered or explicit and marks stale hosts", () => {
	mocks.associations = [
		{ hostId: "host-a", hostLabel: "Linux", projectId: "remote-project", source: "discovered", matchedBy: "repo", stale: true },
		{ hostId: "host-b", hostLabel: "Z600", projectId: "manual-project", source: "explicit", stale: false },
	];
	render(<AggregateProjectBoard projectId="local-project" />);
	const headers = screen.getAllByTestId("aggregate-host-header");
	expect(headers).toHaveLength(2);
	expect(headers[0]).toHaveTextContent("Linux");
	expect(headers[0]).toHaveTextContent("Discovered");
	expect(headers[0]).toHaveTextContent("Stale");
	expect(headers[1]).toHaveTextContent("Z600");
	expect(headers[1]).toHaveTextContent("Explicit");
	expect(headers[1]).not.toHaveTextContent("Stale");
});
