import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { HubHost, HubWorkers } from "../../hooks/useHubHosts";
import { TooltipProvider } from "../ui/tooltip";

vi.mock("../../lib/platform", async (importOriginal) =>
	(await import("../../test/web-build-platform")).webBuildPlatform(await importOriginal()));

import { RemoveWorkerDialog } from "./RemoveWorkerDialog";
import { WorkersSettings } from "./WorkersSettings";

const createdAt = "2026-10-16T08:00:00.000Z";

function host(id: string, extra: Partial<HubHost> = {}): HubHost {
	return { id, label: `worker-${id}`, url: `https://${id}.example`, hostId: id, source: "railway", status: "online", createdAt, removable: true, ...extra };
}

type Handler = (url: string, init?: RequestInit) => Response | Promise<Response>;

let payload: HubWorkers;
let onDelete: Handler;
let sessions: unknown[];
const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
	const url = String(input);
	if (init?.method === "DELETE") return onDelete(url, init);
	if (url.endsWith("/hub/workers")) return Response.json(payload);
	if (url.endsWith("/api/v1/sessions?active=true")) return Response.json({ sessions });
	return new Response(null, { status: 404 });
});

function deleteCalls(): string[] {
	return fetchMock.mock.calls.filter(([, init]) => init?.method === "DELETE").map(([input]) => String(input));
}

beforeEach(() => {
	payload = { workers: [host("rw"), host("man", { source: "manual" })], orphans: [], workersByStatus: {}, limit: 5 };
	sessions = [
		{ id: "s-1", displayName: "Fix login", isTerminated: false, activity: { state: "active" } },
		{ id: "s-2", isTerminated: true, activity: { state: "exited" } },
	];
	onDelete = (url) => {
		const id = decodeURIComponent(url.split("/").pop()!.split("?")[0]);
		payload = { ...payload, workers: payload.workers.filter((worker) => worker.id !== id) };
		return new Response(null, { status: 204 });
	};
	vi.stubGlobal("fetch", fetchMock);
});

afterEach(() => {
	vi.unstubAllGlobals();
	fetchMock.mockClear();
});

function renderWorkers() {
	const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
	return render(
		<QueryClientProvider client={client}>
			<TooltipProvider delayDuration={0}>
				<WorkersSettings rowActions={(entry) => <RemoveWorkerDialog host={entry} />} />
			</TooltipProvider>
		</QueryClientProvider>,
	);
}

function row(id: string): HTMLElement {
	const element = document.querySelector<HTMLElement>(`[data-worker-id="${id}"]`);
	if (!element) throw new Error(`no row for ${id}`);
	return element;
}

async function openDialog(id: string): Promise<HTMLElement> {
	await screen.findByText(`worker-${id}`);
	fireEvent.click(within(row(id)).getByRole("button", { name: "Remove" }));
	return screen.findByRole("dialog");
}

describe("RemoveWorkerDialog", () => {
	it("lists the worker's active sessions and warns that the volume and worktrees go away", async () => {
		renderWorkers();
		const dialog = await openDialog("rw");

		expect(within(dialog).getByText("Remove worker-rw?")).toBeTruthy();
		expect(within(dialog).getByText(/deletes the worker's Railway service and its volume, including every worktree/)).toBeTruthy();
		expect(await within(dialog).findByText("Fix login")).toBeTruthy();
		expect(within(dialog).queryByText("s-2")).toBeNull();
		expect(fetchMock).toHaveBeenCalledWith("/hosts/rw/api/v1/sessions?active=true", expect.anything());
	});

	it("enables Delete only once the label is typed, then removes the worker from the list", async () => {
		renderWorkers();
		const dialog = await openDialog("rw");
		const confirm = within(dialog).getByRole("button", { name: "Delete worker" }) as HTMLButtonElement;
		expect(confirm.disabled).toBe(true);

		fireEvent.change(within(dialog).getByLabelText("Type worker-rw to confirm"), { target: { value: "worker-r" } });
		expect(confirm.disabled).toBe(true);
		fireEvent.change(within(dialog).getByLabelText("Type worker-rw to confirm"), { target: { value: "worker-rw" } });
		expect(confirm.disabled).toBe(false);

		let release!: () => void;
		const pending = new Promise<void>((resolve) => { release = resolve; });
		const original = onDelete;
		onDelete = async (url, init) => { await pending; return original(url, init); };
		fireEvent.click(confirm);

		await waitFor(() => expect(row("rw").querySelector("[data-status]")?.getAttribute("data-status")).toBe("removing"));
		release();
		await waitFor(() => expect(document.querySelector('[data-worker-id="rw"]')).toBeNull());
		expect(screen.queryByRole("dialog")).toBeNull();
		expect(deleteCalls()).toEqual(["/hub/workers/rw"]);
	});

	it("shows the blocking sessions on 409 and retries with ?force=1", async () => {
		onDelete = (url) => url.endsWith("?force=1")
			? new Response(null, { status: 204 })
			: Response.json({ error: "active_sessions", sessions: [{ id: "s-9", displayName: "Long build", activity: { state: "active" } }] }, { status: 409 });
		renderWorkers();
		const dialog = await openDialog("rw");
		fireEvent.change(within(dialog).getByLabelText("Type worker-rw to confirm"), { target: { value: "worker-rw" } });
		fireEvent.click(within(dialog).getByRole("button", { name: "Delete worker" }));

		expect(await within(dialog).findByRole("alert")).toHaveProperty("textContent", expect.stringContaining("Select Force"));
		expect(within(dialog).getByText("Long build")).toBeTruthy();
		await waitFor(() => expect(row("rw").querySelector("[data-status]")?.getAttribute("data-status")).toBe("online"));

		fireEvent.click(within(dialog).getByRole("checkbox", { name: "Force (kill running sessions)" }));
		fireEvent.click(within(dialog).getByRole("button", { name: "Delete worker" }));
		await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
		expect(deleteCalls()).toEqual(["/hub/workers/rw", "/hub/workers/rw?force=1"]);
	});

	it("suggests force when the hub could not check the sessions", async () => {
		onDelete = () => Response.json({ error: "session_check_failed" }, { status: 409 });
		renderWorkers();
		const dialog = await openDialog("rw");
		fireEvent.change(within(dialog).getByLabelText("Type worker-rw to confirm"), { target: { value: "worker-rw" } });
		fireEvent.click(within(dialog).getByRole("button", { name: "Delete worker" }));

		expect((await within(dialog).findByRole("alert")).textContent).toMatch(/could not check this worker's sessions. Select Force/);
	});

	it("explains a refused Railway delete and keeps the record as failed", async () => {
		const lastError = "Railway could not delete this worker; the registry record was retained for cleanup";
		onDelete = () => {
			const worker = host("rw", { status: "failed", lastError });
			payload = { ...payload, workers: payload.workers.map((entry) => (entry.id === "rw" ? worker : entry)) };
			return Response.json({ error: "worker_delete_failed", worker }, { status: 502 });
		};
		renderWorkers();
		const dialog = await openDialog("rw");
		fireEvent.change(within(dialog).getByLabelText("Type worker-rw to confirm"), { target: { value: "worker-rw" } });
		fireEvent.click(within(dialog).getByRole("button", { name: "Delete worker" }));

		expect((await within(dialog).findByRole("alert")).textContent).toMatch(/Railway refused to delete this worker.*project token cannot delete services or volumes/);
		await waitFor(() => expect(row("rw").querySelector("[data-status]")?.getAttribute("data-status")).toBe("failed"));
	});

	it("only removes the registry record of a manual host and says so", async () => {
		renderWorkers();
		const dialog = await openDialog("man");

		expect(within(dialog).getByText(/only removes the worker from the hub's registry/)).toBeTruthy();
		expect(within(dialog).queryByRole("checkbox")).toBeNull();
		expect(within(dialog).queryByRole("textbox")).toBeNull();
		expect(within(dialog).queryByTestId("worker-sessions")).toBeNull();
		fireEvent.click(within(dialog).getByRole("button", { name: "Remove record" }));

		await waitFor(() => expect(document.querySelector('[data-worker-id="man"]')).toBeNull());
		expect(deleteCalls()).toEqual(["/hub/hosts/man"]);
	});

	it("clears the typed label and force when the dialog closes", async () => {
		renderWorkers();
		let dialog = await openDialog("rw");
		fireEvent.change(within(dialog).getByLabelText("Type worker-rw to confirm"), { target: { value: "worker-rw" } });
		fireEvent.click(within(dialog).getByRole("checkbox", { name: "Force (kill running sessions)" }));
		fireEvent.click(within(dialog).getByRole("button", { name: "Cancel" }));
		await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());

		dialog = await openDialog("rw");
		expect((within(dialog).getByLabelText("Type worker-rw to confirm") as HTMLInputElement).value).toBe("");
		expect(within(dialog).getByRole("checkbox", { name: "Force (kill running sessions)" }).getAttribute("data-state")).toBe("unchecked");
		expect(deleteCalls()).toEqual([]);
	});
});
