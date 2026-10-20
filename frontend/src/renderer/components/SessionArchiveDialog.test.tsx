import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { WorkspaceSession } from "../types/workspace";
import { SessionArchiveDialog } from "./SessionArchiveDialog";

const baseSession = {
	id: "orchestrator-1",
	workspaceId: "project-1",
	workspaceName: "project",
	title: "Orchestrator",
	provider: "codex",
	kind: "orchestrator",
	status: "working",
	updatedAt: "2026-10-19T00:00:00Z",
	prs: [],
} satisfies WorkspaceSession;

function renderDialog(session: WorkspaceSession) {
	render(
		<SessionArchiveDialog
			onConfirm={() => undefined}
			onOpenChange={() => undefined}
			open
			session={session}
			trigger={<button type="button">Open</button>}
		/>,
	);
}

describe("SessionArchiveDialog", () => {
	it("keeps dedicated CLI restore copy for a local orchestrator", () => {
		renderDialog(baseSession);

		const dialog = screen.getByRole("dialog", {
			name: "Kill this orchestrator?",
		});
		expect(dialog).toHaveTextContent("ao session restore <id>");
	});

	it("keeps cloud teardown copy for a cloud orchestrator", () => {
		renderDialog({
			...baseSession,
			cloud: { orgId: "org-1" },
		});

		const dialog = screen.getByRole("dialog", {
			name: "Kill this orchestrator?",
		});
		expect(dialog).toHaveTextContent(
			"You can always restore it from the Archive section later",
		);
		expect(dialog).not.toHaveTextContent("ao session restore");
	});
});
