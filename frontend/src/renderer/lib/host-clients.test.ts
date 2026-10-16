import { afterEach, expect, it, vi } from "vitest";

const remotes = vi.hoisted(() => ({ connect: vi.fn(), disconnect: vi.fn(async () => undefined) }));
vi.mock("./bridge", () => ({ aoBridge: { remotes } }));

import { baseUrlForHost, clientForHost, connectHost, connectedHosts, disconnectHost, isQuickTunnelHost, subscribeConnectedHosts } from "./host-clients";

afterEach(async () => {
	for (const hostId of connectedHosts()) await disconnectHost(hostId);
	remotes.connect.mockReset();
});

it("never treats the local sentinel as a remote host", () => {
	expect(() => clientForHost("local")).toThrow(/not a remote host/);
});

it("re-pairing an address removes its old host identity from the active map", async () => {
	const url = "http://box:3001";
	remotes.connect
		.mockResolvedValueOnce({ hostId: "h_old", label: "Box", url, base: "http://127.0.0.1:4000/old" })
		.mockResolvedValueOnce({ hostId: "h_new", label: "Box", url, base: "http://127.0.0.1:4001/new" });
	await connectHost(url);
	await connectHost(url);
	expect(connectedHosts()).toEqual(["h_new"]);
});

it("recognizes only a connected Cloudflare quick-tunnel hostname", async () => {
	remotes.connect.mockResolvedValue({ hostId: "h_tunnel", label: "Tunnel", url: "https://box.trycloudflare.com", base: "http://127.0.0.1:4000" });
	await connectHost("https://box.trycloudflare.com");
	expect(isQuickTunnelHost("h_tunnel")).toBe(true);
	expect(isQuickTunnelHost("missing")).toBe(false);
	remotes.connect.mockResolvedValue({ hostId: "h_direct", label: "Direct", url: "https://box.trycloudflare.com.evil.example", base: "http://127.0.0.1:4001" });
	await connectHost("https://box.trycloudflare.com.evil.example");
	expect(isQuickTunnelHost("h_direct")).toBe(false);
});

it("publishes an endpoint change even when its loopback proxy keeps the same base", async () => {
	const changed = vi.fn();
	const stop = subscribeConnectedHosts(changed);
	remotes.connect.mockResolvedValue({ hostId: "h_box", label: "Box", url: "http://box:3001", base: "http://127.0.0.1:4000" });
	await connectHost("http://box:3001");
	changed.mockClear();
	remotes.connect.mockResolvedValue({ hostId: "h_box", label: "Box", url: "https://box.trycloudflare.com", base: "http://127.0.0.1:4000" });
	await connectHost("https://box.trycloudflare.com");
	expect(changed).toHaveBeenCalledOnce();
	expect(isQuickTunnelHost("h_box")).toBe(true);
	stop();
});

it("keeps a web host's same-origin prefix as its base and resolves it for REST requests", async () => {
	const fetchMock = vi.fn<typeof fetch>(async () => new Response(JSON.stringify({ projects: [] }), { status: 200, headers: { "Content-Type": "application/json" } }));
	vi.stubGlobal("fetch", fetchMock);
	try {
		remotes.connect.mockResolvedValue({ hostId: "h_web", label: "Worker", url: "http://worker:3011", base: "/hosts/h_web" });
		await connectHost("http://worker:3011", "h_web");
		expect(baseUrlForHost("h_web")).toBe("/hosts/h_web");
		await clientForHost("h_web").GET("/api/v1/projects");
		const request = fetchMock.mock.calls[0][0] as Request;
		expect(request.url).toBe(`${window.location.origin}/hosts/h_web/api/v1/projects`);
	} finally {
		vi.unstubAllGlobals();
	}
});
