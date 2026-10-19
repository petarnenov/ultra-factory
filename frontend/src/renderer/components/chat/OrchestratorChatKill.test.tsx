import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { WorkspaceSession } from "../../types/workspace";
import { TooltipProvider } from "../ui/tooltip";

const { navigateMock, postMock } = vi.hoisted(() => ({
	navigateMock: vi.fn(),
	postMock: vi.fn(),
}));

vi.mock("@tanstack/react-router", () => ({
	useNavigate: () => navigateMock,
}));

vi.mock("../../lib/api-client", () => ({
	apiClient: {
		GET: vi.fn(),
		PATCH: vi.fn(),
		POST: postMock,
		PUT: vi.fn(),
	},
	getApiBaseUrl: () => "http://127.0.0.1:3001",
	hasTrustedApiBaseUrl: () => false,
	subscribeApiBaseUrl: () => () => {},
	apiErrorMessage: (error: unknown, fallback = "Request failed") =>
		error instanceof Error ? error.message : fallback,
}));

import { OrchestratorChatKill } from "./OrchestratorChatKill";

const session = {
	id: "orchestrator-1",
	workspaceId: "project-1",
	workspaceName: "project",
	title: "Orchestrator",
	provider: "codex",
	kind: "orchestrator",
	mode: "chat",
	status: "working",
	updatedAt: "2026-10-19T00:00:00Z",
	prs: [],
} satisfies WorkspaceSession;

function Wrapper({ children }: { children: ReactNode }) {
	const client = new QueryClient({
		defaultOptions: { mutations: { retry: false }, queries: { retry: false } },
	});
	return (
		<QueryClientProvider client={client}>
			<TooltipProvider>{children}</TooltipProvider>
		</QueryClientProvider>
	);
}

describe("OrchestratorChatKill", () => {
	beforeEach(() => {
		navigateMock.mockReset();
		postMock.mockReset();
	});

	it("terminates only after confirmation and explains the restore path", async () => {
		postMock.mockReturnValue(new Promise(() => {}));
		render(
			<Wrapper>
				<OrchestratorChatKill session={session} />
			</Wrapper>,
		);

		await userEvent.click(
			screen.getByRole("button", { name: "Kill orchestrator" }),
		);
		const dialog = screen.getByRole("dialog", {
			name: "Kill this orchestrator?",
		});
		expect(dialog).toHaveTextContent("terminates the orchestrator session");
		expect(dialog).toHaveTextContent("ao session restore <id>");
		expect(postMock).not.toHaveBeenCalled();

		await userEvent.click(
			within(dialog).getByRole("button", {
				name: "Confirm, kill orchestrator",
			}),
		);

		await waitFor(() =>
			expect(postMock).toHaveBeenCalledWith(
				"/api/v1/sessions/{sessionId}/kill",
				{ params: { path: { sessionId: session.id } } },
			),
		);
		expect(navigateMock).toHaveBeenCalledWith({
			to: "/projects/$projectId",
			params: { projectId: session.workspaceId },
		});
	});
});
