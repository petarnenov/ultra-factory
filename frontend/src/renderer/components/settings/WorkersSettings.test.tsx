import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { HUB_BUSY_POLL_MS, HUB_IDLE_POLL_MS, hubHostsPollInterval, type HubHost, type HubWorkers } from "../../hooks/useHubHosts";
import { TooltipProvider } from "../ui/tooltip";

vi.mock("../../lib/platform", async (importOriginal) =>
	(await import("../../test/web-build-platform")).webBuildPlatform(await importOriginal()));

import { WorkersSettings } from "./WorkersSettings";

const createdAt = "2026-10-16T08:00:00.000Z";

function host(id: string, status: HubHost["status"], extra: Partial<HubHost> = {}): HubHost {
	return { id, label: `Worker ${id}`, url: `https://${id}.example`, source: "railway", status, createdAt, removable: true, ...extra };
}

const allStatuses: HubHost[] = [
	host("prov", "provisioning"),
	host("on", "online", { hostId: "on", source: "manual" }),
	host("unr", "unreachable"),
	host("auth", "auth_failed"),
	host("rem", "removing"),
	host("fail", "failed", { lastError: "Railway deployment failed" }),
];

let payload: HubWorkers;
const fetchMock = vi.fn(async (input: RequestInfo | URL, _init?: RequestInit) => {
	const url = String(input);
	if (url.endsWith("/hub/workers")) return Response.json(payload);
	if (url.endsWith("/hosts/on/api/v1/sessions?active=true")) {
		return Response.json({ sessions: [{ id: "a", isTerminated: false }, { id: "b", isTerminated: false }, { id: "c", isTerminated: true }] });
	}
	return new Response(null, { status: 404 });
});

beforeEach(() => {
	payload = {
		workers: [{ ...host("hub", "online"), source: "hub", label: "Hub", removable: false }, ...allStatuses],
		orphans: [],
		workersByStatus: {},
		limit: 5,
	};
	vi.stubGlobal("fetch", fetchMock);
});

afterEach(() => {
	vi.unstubAllGlobals();
	fetchMock.mockClear();
});

function renderWorkers(props: Parameters<typeof WorkersSettings>[0] = {}) {
	const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
	return render(
		<QueryClientProvider client={client}>
			<TooltipProvider delayDuration={0}>
				<WorkersSettings {...props} />
			</TooltipProvider>
		</QueryClientProvider>,
	);
}

function row(id: string): HTMLElement {
	const element = document.querySelector<HTMLElement>(`[data-worker-id="${id}"]`);
	if (!element) throw new Error(`no row for ${id}`);
	return element;
}

describe("WorkersSettings", () => {
	it("renders every worker status and leaves out the hub's own entry", async () => {
		renderWorkers();
		await screen.findByText("Worker prov");

		expect(screen.queryByText("Hub")).toBeNull();
		expect(screen.getAllByTestId("worker-row")).toHaveLength(allStatuses.length);
		const expected: Record<string, string> = {
			prov: "Provisioning",
			on: "Online",
			unr: "Unreachable",
			auth: "Authentication failed",
			rem: "Removing",
			fail: "Failed",
		};
		for (const [id, label] of Object.entries(expected)) {
			expect(within(row(id)).getByText(label)).toBeVisible();
		}
		// Provisioning and removing carry the spinner; settled states do not.
		for (const id of ["prov", "rem"]) expect(row(id).querySelector(".animate-spin")).not.toBeNull();
		for (const id of ["on", "unr", "auth", "fail"]) expect(row(id).querySelector(".animate-spin")).toBeNull();
	});

	it("shows source, created date, and active sessions for an online worker", async () => {
		renderWorkers();
		await screen.findByText("Worker on");

		expect(within(row("on")).getByText(/^Manual · Created /)).toBeVisible();
		expect(within(row("prov")).getByText(/^Railway · Created /)).toBeVisible();
		expect(await within(row("on")).findByText("2 active sessions")).toBeVisible();
		expect(within(row("unr")).queryByText(/active session/)).toBeNull();
		const sessionCalls = fetchMock.mock.calls.map(([input]) => String(input)).filter((url) => url.includes("/api/v1/sessions"));
		expect(sessionCalls).toEqual(["/hosts/on/api/v1/sessions?active=true"]);
	});

	it("explains a failed worker with its lastError tooltip", async () => {
		renderWorkers();
		const badge = await within(await waitFor(() => row("fail"))).findByText("Failed");

		await act(async () => { fireEvent.focus(badge); });
		expect((await screen.findAllByText("Railway deployment failed")).length).toBeGreaterThan(0);
	});

	it("warns about an idle worker with the configured threshold", async () => {
		payload.idle = [{ id: "on", idleSince: "2026-10-17T00:00:00.000Z" }];
		payload.idleStopHours = 6;
		renderWorkers();
		const warning = await within(await waitFor(() => row("on"))).findByTestId("worker-idle");

		expect(warning).toHaveTextContent("Idle 6+ h");
		expect(within(row("unr")).queryByTestId("worker-idle")).toBeNull();
		await act(async () => { fireEvent.focus(warning); });
		expect((await screen.findAllByText(/No active session for 6 hours/)).length).toBeGreaterThan(0);
	});

	it("shows no idle warning when the idle check is off", async () => {
		payload.idle = [{ id: "on", idleSince: "2026-10-17T00:00:00.000Z" }];
		renderWorkers();
		await screen.findByText("Worker on");

		expect(screen.queryByTestId("worker-idle")).toBeNull();
	});

	it("lists orphans read-only with Adopt and Delete disabled", async () => {
		payload.orphans = [{ id: "svc-123", name: "ao-worker-deadbeef" }];
		renderWorkers();
		const orphan = await screen.findByTestId("worker-orphan");

		expect(within(orphan).getByText("ao-worker-deadbeef")).toBeVisible();
		expect(within(orphan).getByText("svc-123")).toBeVisible();
		expect(screen.getByText(/Adopt and Delete are unavailable/)).toBeVisible();
		for (const name of ["Adopt", "Delete"]) {
			const button = within(orphan).getByRole("button", { name });
			expect(button).toBeDisabled();
			fireEvent.click(button);
		}
		const requests = fetchMock.mock.calls.map(([input, init]) => `${init?.method ?? "GET"} ${String(input)}`);
		expect(requests.every((request) => request.startsWith("GET "))).toBe(true);
	});

	it("shows an empty state and a load error with retry", async () => {
		payload.workers = [];
		const { unmount } = renderWorkers();
		expect(await screen.findByText("No workers yet.")).toBeVisible();
		unmount();

		fetchMock.mockImplementationOnce(async () => Response.json({ error: "unauthorized", message: "Sign in again" }, { status: 401 }));
		renderWorkers();
		expect(await screen.findByRole("alert")).toHaveTextContent("Sign in again");
		fireEvent.click(screen.getByRole("button", { name: "Retry" }));
		expect(await screen.findByText("No workers yet.")).toBeVisible();
	});

	it("renders the slots RW-3.3.2 and RW-3.3.3 plug into", async () => {
		const onOpenHost = vi.fn();
		renderWorkers({ addWorkerAction: <button type="button">Add worker</button>, rowActions: (worker) => <button type="button">Remove {worker.id}</button>, onOpenHost });
		await screen.findByText("Worker on");

		expect(screen.getByRole("button", { name: "Add worker" })).toBeVisible();
		expect(within(row("fail")).getByRole("button", { name: "Remove fail" })).toBeVisible();
		fireEvent.click(within(row("on")).getByRole("button", { name: "Open" }));
		expect(onOpenHost).toHaveBeenCalledWith(expect.objectContaining({ id: "on" }));
		expect(within(row("unr")).queryByRole("button", { name: "Open" })).toBeNull();
	});
});

describe("hubHostsPollInterval", () => {
	const data = (statuses: HubHost["status"][]): HubWorkers => ({ workers: statuses.map((status, index) => host(String(index), status)), orphans: [], workersByStatus: {}, limit: 5 });

	it("polls every 5 s while a worker is provisioning or removing", () => {
		expect(hubHostsPollInterval(data(["online", "provisioning"]))).toBe(HUB_BUSY_POLL_MS);
		expect(hubHostsPollInterval(data(["removing"]))).toBe(HUB_BUSY_POLL_MS);
		expect(HUB_BUSY_POLL_MS).toBe(5_000);
	});

	it("polls every 30 s otherwise", () => {
		expect(hubHostsPollInterval(undefined)).toBe(HUB_IDLE_POLL_MS);
		expect(hubHostsPollInterval(data(["online", "failed", "unreachable", "auth_failed"]))).toBe(HUB_IDLE_POLL_MS);
		expect(HUB_IDLE_POLL_MS).toBe(30_000);
	});
});
