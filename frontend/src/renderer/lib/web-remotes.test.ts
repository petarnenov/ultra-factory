import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createWebRemotesBridge, webHostBaseUrl } from "./web-remotes";

const fetchMock = vi.fn<typeof fetch>();

function json(status: number, body: unknown): Response {
	return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
}

const hub = { id: "hub", label: "hub", url: "http://127.0.0.1:3011", hostId: "hub", source: "hub", status: "online", removable: false };
const worker = { id: "h_worker", label: "Worker 1", url: "http://worker-1.railway.internal:3011", hostId: "h_worker", source: "manual", status: "online", removable: true };

function hostsResponse(...hosts: unknown[]): Response {
	return json(200, { hosts });
}

beforeEach(() => {
	fetchMock.mockReset();
	vi.stubGlobal("fetch", fetchMock);
});

afterEach(() => {
	vi.unstubAllGlobals();
});

describe("web remotes bridge", () => {
	const remotes = createWebRemotesBridge(() => "");

	it("lists the registry's hosts without the hub's own entry", async () => {
		fetchMock.mockResolvedValueOnce(hostsResponse(hub, worker));

		await expect(remotes.list()).resolves.toEqual([{ hostId: "h_worker", label: "Worker 1", url: worker.url, allowInsecureHTTP: false }]);
		expect(fetchMock).toHaveBeenCalledWith("/hub/hosts", expect.objectContaining({ cache: "no-store" }));
	});

	it("prefixes the hub routes with an explicit API base URL", async () => {
		fetchMock.mockResolvedValueOnce(hostsResponse());

		await createWebRemotesBridge(() => "https://hub.example.test").list();

		expect(fetchMock).toHaveBeenCalledWith("https://hub.example.test/hub/hosts", expect.anything());
	});

	it("surfaces a failed list with the BFF's message", async () => {
		fetchMock.mockResolvedValueOnce(json(401, { error: "unauthorized" }));

		await expect(remotes.list()).rejects.toThrow("unauthorized");
	});

	it("adds a host through POST /hub/hosts", async () => {
		fetchMock.mockResolvedValueOnce(json(201, { host: worker }));

		await expect(remotes.add({ label: "Worker 1", url: worker.url, password: "secret" })).resolves.toBe("online");
		const [url, init] = fetchMock.mock.calls[0];
		expect(url).toBe("/hub/hosts");
		expect(init).toMatchObject({ method: "POST", headers: { "Content-Type": "application/json" } });
		expect(JSON.parse(String(init?.body))).toEqual({ label: "Worker 1", url: worker.url, password: "secret" });
	});

	it.each([
		[422, { error: "host_credential_rejected" }, "unauthorized"],
		[502, { error: "host_unreachable" }, "offline"],
		[502, { error: "unsupported_api_version" }, "incompatible"],
	])("maps a refused add (%i %o) to %s", async (status, body, health) => {
		fetchMock.mockResolvedValueOnce(json(status, body));

		await expect(remotes.add({ label: "Worker 1", url: worker.url, password: "secret" })).resolves.toBe(health);
	});

	it("rejects an invalid or duplicate add with the BFF's message", async () => {
		fetchMock.mockResolvedValueOnce(json(409, { error: "label_taken", message: "a host with this label already exists" }));

		await expect(remotes.add({ label: "Worker 1", url: worker.url, password: "secret" })).rejects.toThrow("a host with this label already exists");
	});

	it("removes a host by its registry id", async () => {
		fetchMock.mockResolvedValueOnce(hostsResponse(hub, worker)).mockResolvedValueOnce(new Response(null, { status: 204 }));

		await remotes.remove(worker.url);

		expect(fetchMock).toHaveBeenLastCalledWith("/hub/hosts/h_worker", expect.objectContaining({ method: "DELETE" }));
	});

	it("connects an online host at its same-origin /hosts/<id> prefix", async () => {
		fetchMock.mockResolvedValueOnce(hostsResponse(hub, worker)).mockResolvedValueOnce(json(200, { host: worker }));

		await expect(remotes.connect(worker.url, "h_worker")).resolves.toEqual({
			hostId: "h_worker",
			label: "Worker 1",
			url: worker.url,
			base: "/hosts/h_worker",
		});
		expect(fetchMock).toHaveBeenLastCalledWith("/hub/hosts/h_worker/probe", expect.objectContaining({ method: "POST" }));
	});

	it.each([
		["unreachable", "offline"],
		["auth_failed", "unauthorized"],
	])("reports a %s probe with the desktop's %s wording", async (status, health) => {
		fetchMock.mockResolvedValueOnce(hostsResponse(worker)).mockResolvedValueOnce(json(200, { host: { ...worker, status } }));

		await expect(remotes.connect(worker.url, "h_worker")).rejects.toThrow(`host ${worker.url} is ${health}`);
	});

	it("does not connect a host the registry does not have", async () => {
		fetchMock.mockResolvedValueOnce(hostsResponse(hub));

		await expect(remotes.connect(worker.url)).rejects.toThrow(`no saved host for ${worker.url}`);
		expect(fetchMock).toHaveBeenCalledOnce();
	});

	it("keeps desktop-only account actions unavailable", async () => {
		await expect(remotes.issueAccountToken(worker.url)).rejects.toThrow("desktop app");
		await expect(remotes.update(worker.url, { label: "x" })).rejects.toThrow("desktop app");
	});

	it("builds the host prefix from the API base URL", () => {
		expect(webHostBaseUrl("", "h_worker")).toBe("/hosts/h_worker");
		expect(webHostBaseUrl("https://hub.example.test", "h_worker")).toBe("https://hub.example.test/hosts/h_worker");
	});
});
