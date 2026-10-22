import { describe, expect, it, vi } from "vitest";
import { chmod, mkdtemp, readFile, writeFile } from "node:fs/promises";
import { createServer } from "node:http";
import { homedir, tmpdir } from "node:os";
import { join } from "node:path";
import { NO_TAILSCALE_ADDRESS, registerRemotesIpc, remotesFilePath, tailnetAccountUrl, type RemotesIpcDeps } from "./remotes-main";
import { RemoteRegistry } from "./remote-registry";
import { createLocalHostIdentity } from "./remote-request";

type Handler = (event: unknown, ...args: unknown[]) => Promise<unknown>;

// ipcMain stand-in: records what was registered and lets a test invoke it.
function fakeIpc() {
	const handlers = new Map<string, Handler>();
	return {
		ipcMain: { handle: (channel: string, handler: Handler) => void handlers.set(channel, handler) },
		invoke: (channel: string, ...args: unknown[]) => {
			const handler = handlers.get(channel);
			if (!handler) throw new Error(`no handler for ${channel}`);
			return handler({}, ...args);
		},
		channels: () => [...handlers.keys()].sort(),
	};
}

const TEST_ACCOUNT = "test-account";
const signedIn = async () => {};

type TestDeps = Omit<RemotesIpcDeps, "getAccountId" | "localIdentity" | "daemonBaseUrl"> & Partial<Pick<RemotesIpcDeps, "getAccountId" | "localIdentity" | "daemonBaseUrl">>;

// Discovery is off unless a test injects it: the daemon is not running here.
function registerRemotes(ipcMain: Parameters<typeof registerRemotesIpc>[0], deps: TestDeps): void {
	registerRemotesIpc(ipcMain, {
		localIdentity: async () => "h_this_mac", daemonBaseUrl: () => null, discover: async () => null,
		updateTailnet: async () => { throw new Error("daemon not running"); },
		...deps, getAccountId: deps.getAccountId ?? (async () => TEST_ACCOUNT),
	});
}

async function tempFile(): Promise<string> {
	const dir = await mkdtemp(join(tmpdir(), "ao-remotes-main-"));
	const path = join(dir, "remotes.json");
	await writeFile(path, `{"remotes":[{"hostId":"h_workbox","label":"workbox","url":"http://192.0.2.1:1","password":"old","accountUserId":"${TEST_ACCOUNT}"}]}`, "utf8");
	await chmod(path, 0o600);
	return path;
}

describe("remotesFilePath", () => {
	it("keeps an isolated AO_DATA_DIR out of the real home credentials", () => {
		vi.stubEnv("AO_DATA_DIR", "/tmp/ao-remote-proof");
		try {
			expect(remotesFilePath()).toBe("/tmp/ao-remote-proof/remotes.json");
		} finally {
			vi.unstubAllEnvs();
		}
	});

	it("shares the machine-level file with the daemons and honors AO_REMOTES_FILE", () => {
		const home = homedir();
		try {
			vi.stubEnv("AO_DATA_DIR", join(home, ".ao", "dev", "data"));
			expect(remotesFilePath()).toBe(join(home, ".ao", "remotes.json"));
			vi.stubEnv("AO_DATA_DIR", "");
			vi.stubEnv("AO_REMOTES_FILE", "/tmp/ao-remote-override.json");
			expect(remotesFilePath()).toBe("/tmp/ao-remote-override.json");
		} finally {
			vi.unstubAllEnvs();
		}
	});
});

describe("registerRemotesIpc", () => {
	it("lists saved metadata while a remote probe is pending", async () => {
		const file = await tempFile();
		let finish!: (health: "offline") => void;
		const probe = vi.fn(() => new Promise<"offline">((resolve) => { finish = resolve; }));
		const ipc = fakeIpc();
		registerRemotes(ipc.ipcMain, {
			file, registry: new RemoteRegistry(async () => { throw new Error("unused"); }),
			requireAccount: signedIn, identity: async () => "h_workbox", probe,
			discover: async () => ({ available: true, localHostIds: [], hosts: [] }),
		});
		const connection = ipc.invoke("remotes:connect", "http://192.0.2.1:1");
		const failedConnection = expect(connection).rejects.toThrow("offline");
		await vi.waitFor(() => expect(probe).toHaveBeenCalled());
		try {
			await expect(ipc.invoke("remotes:list")).resolves.toEqual([
				{ hostId: "h_workbox", label: "workbox", url: "http://192.0.2.1:1", allowInsecureHTTP: false },
			]);
		} finally {
			finish("offline");
			await failedConnection;
		}
	});

	it("keeps a locally paired LAN URL when the account directory returns the Tailscale URL", async () => {
		const file = await tempFile();
		const ipc = fakeIpc();
		registerRemotes(ipc.ipcMain, { file, registry: new RemoteRegistry(async () => { throw new Error("unused"); }), requireAccount: signedIn });
		await ipc.invoke("remotes:importAccountHost", TEST_ACCOUNT, { hostId: "h_workbox", label: "New name", url: "http://100.100.160.17:57451", password: "a".repeat(64) });
		expect(JSON.parse(await readFile(file, "utf8")).remotes).toEqual([
			{ hostId: "h_workbox", label: "New name", url: "http://192.0.2.1:1", password: "old", accountUserId: TEST_ACCOUNT },
		]);
	});

	it("hides this installation without deleting its account record or original password", async () => {
		const file = await tempFile();
		const original = await readFile(file, "utf8");
		const ipc = fakeIpc();
		registerRemotes(ipc.ipcMain, { file, registry: new RemoteRegistry(async () => { throw new Error("unused"); }), requireAccount: signedIn, localIdentity: async () => "h_workbox" });
		await expect(ipc.invoke("remotes:list")).resolves.toEqual([]);
		expect(await readFile(file, "utf8")).toBe(original);
		await ipc.invoke("remotes:importAccountHost", TEST_ACCOUNT, { hostId: "h_workbox", label: "New name", url: "https://rotated.example", password: "a".repeat(64) });
		await ipc.invoke("remotes:pruneAccountHosts", TEST_ACCOUNT, ["h_workbox"]);
		await expect(ipc.invoke("remotes:list")).resolves.toEqual([]);
		expect(JSON.parse(await readFile(file, "utf8")).remotes).toEqual([
			{ hostId: "h_workbox", label: "New name", url: "https://rotated.example", password: "old", accountUserId: TEST_ACCOUNT },
		]);
	});

	it.each(["remotes:add", "remotes:update", "remotes:connect", "remotes:issueAccountToken", "remotes:previewUrl", "remotes:resolvePreviewUrl"])("rejects direct self access through %s before credentials or proxies", async (channel) => {
		const file = await tempFile();
		const original = await readFile(file, "utf8");
		const probe = vi.fn(async () => "online" as const);
		const start = vi.fn(async () => { throw new Error("proxy must not start"); });
		const ipc = fakeIpc();
		registerRemotes(ipc.ipcMain, { file, registry: new RemoteRegistry(start), requireAccount: signedIn, localIdentity: async () => "h_workbox", identity: async () => "h_workbox", probe });
		const args = channel === "remotes:add" ? [{ label: "self", url: "https://self.example", password: "new" }]
			: channel === "remotes:update" ? ["http://192.0.2.1:1", { password: "new" }]
			: channel.includes("Preview") || channel === "remotes:previewUrl" ? ["h_workbox", "s_1", "http://localhost:3000"]
			: ["http://192.0.2.1:1"];
		await expect(ipc.invoke(channel, ...args)).rejects.toThrow(/itself/);
		expect(probe).not.toHaveBeenCalled();
		expect(start).not.toHaveBeenCalled();
		expect(await readFile(file, "utf8")).toBe(original);
	});

	it("refuses an address that resolves to self under another saved host ID before authentication", async () => {
		const file = await tempFile();
		const probe = vi.fn(async () => "online" as const);
		const ipc = fakeIpc();
		registerRemotes(ipc.ipcMain, { file, registry: new RemoteRegistry(async () => { throw new Error("unused"); }), requireAccount: signedIn, identity: async () => "h_this_mac", probe });
		await expect(ipc.invoke("remotes:update", "http://192.0.2.1:1", { url: "https://self.example" })).rejects.toThrow(/itself/);
		await expect(ipc.invoke("remotes:issueAccountToken", "http://192.0.2.1:1")).rejects.toThrow(/itself/);
		expect(probe).not.toHaveBeenCalled();
	});

	it("defers new connections while identity is unknown, then recovers without changing the password", async () => {
		const file = await tempFile();
		const identity = vi.fn(async () => "h_workbox");
		const probe = vi.fn(async () => "online" as const);
		let localHostId: string | null = null;
		const start = vi.fn(async () => ({ base: "http://127.0.0.1:5000/token", previewUrl: (_sessionId: string, sourceUrl: string) => sourceUrl, resolvePreviewUrl: (_sessionId: string, viewedUrl: string) => viewedUrl, close: async () => {} }));
		const ipc = fakeIpc();
		registerRemotes(ipc.ipcMain, { file, registry: new RemoteRegistry(start), requireAccount: signedIn, identity, probe, localIdentity: async () => localHostId });
		await expect(ipc.invoke("remotes:list")).resolves.toHaveLength(1);
		await expect(ipc.invoke("remotes:connect", "http://192.0.2.1:1")).rejects.toThrow(/ is offline$/);
		expect(identity).not.toHaveBeenCalled();
		expect(probe).not.toHaveBeenCalled();
		expect(start).not.toHaveBeenCalled();
		localHostId = "h_this_mac";
		await expect(ipc.invoke("remotes:connect", "http://192.0.2.1:1")).resolves.toMatchObject({ hostId: "h_workbox" });
		expect(start).toHaveBeenCalledWith(expect.objectContaining({ password: "old" }));
		localHostId = null;
		await expect(ipc.invoke("remotes:connect", "http://192.0.2.1:1")).resolves.toMatchObject({ hostId: "h_workbox" });
		expect(start).toHaveBeenCalledOnce();
		expect(probe).toHaveBeenCalledOnce();
	});

	it.each(["remotes:add", "remotes:update", "remotes:issueAccountToken"])("defers %s without presenting a credential when local identity is unknown", async (channel) => {
		const file = await tempFile();
		const original = await readFile(file, "utf8");
		const identity = vi.fn(async () => "h_workbox");
		const probe = vi.fn(async () => "online" as const);
		const ipc = fakeIpc();
		registerRemotes(ipc.ipcMain, { file, registry: new RemoteRegistry(async () => { throw new Error("unused"); }), requireAccount: signedIn, localIdentity: async () => null, identity, probe });
		const args = channel === "remotes:add" ? [{ label: "Other", url: "https://other.example", password: "new" }]
			: channel === "remotes:update" ? ["http://192.0.2.1:1", { password: "new" }] : ["http://192.0.2.1:1"];
		await expect(ipc.invoke(channel, ...args)).rejects.toThrow(/ is offline$/);
		expect(identity).not.toHaveBeenCalled();
		expect(probe).not.toHaveBeenCalled();
		expect(await readFile(file, "utf8")).toBe(original);
	});

	it.each(["http://192.0.2.1:3011", "https://other.trycloudflare.com:443"])("preserves another host's pairing through %s", async (url) => {
		const file = await tempFile();
		await writeFile(file, JSON.stringify({ remotes: [{ hostId: "h_workbox", label: "Other", url, password: "original", accountUserId: TEST_ACCOUNT }] }));
		const original = await readFile(file, "utf8");
		const probe = vi.fn(async () => "online" as const);
		const start = vi.fn(async () => ({ base: "http://127.0.0.1:5000/token", previewUrl: (_sessionId: string, sourceUrl: string) => sourceUrl, resolvePreviewUrl: (_sessionId: string, viewedUrl: string) => viewedUrl, close: async () => {} }));
		const ipc = fakeIpc();
		registerRemotes(ipc.ipcMain, { file, registry: new RemoteRegistry(start), requireAccount: signedIn, identity: async () => "h_workbox", probe });
		await expect(ipc.invoke("remotes:connect", url)).resolves.toMatchObject({ hostId: "h_workbox", url });
		expect(probe).toHaveBeenCalledWith(expect.objectContaining({ url, password: "original" }));
		expect(start).toHaveBeenCalledWith(expect.objectContaining({ url, password: "original" }));
		expect(await readFile(file, "utf8")).toBe(original);
	});

	it("retains an existing verified remote if local identity becomes unavailable during its probe", async () => {
		const file = await tempFile();
		const close = vi.fn(async () => {});
		const registry = new RemoteRegistry(async () => ({ base: "http://127.0.0.1:5000/token", previewUrl: (_sessionId, sourceUrl) => sourceUrl, resolvePreviewUrl: (_sessionId, viewedUrl) => viewedUrl, close }));
		await registry.connect({ hostId: "h_workbox", label: "workbox", url: "http://192.0.2.1:1", password: "old" });
		let localHostId: string | null = "h_this_mac";
		const probe = vi.fn(async () => { localHostId = null; return "online" as const; });
		const ipc = fakeIpc();
		registerRemotes(ipc.ipcMain, { file, registry, requireAccount: signedIn, identity: async () => "h_workbox", localIdentity: async () => localHostId, probe });
		await expect(ipc.invoke("remotes:connect", "http://192.0.2.1:1")).resolves.toMatchObject({ hostId: "h_workbox" });
		expect(close).not.toHaveBeenCalled();
	});

	it("invalidates identity on a same-port restart before allowing another remote connection", async () => {
		const file = await tempFile();
		let generation = 1;
		const localProbe = vi.fn().mockResolvedValueOnce("h_this_mac").mockRejectedValueOnce(new Error("starting")).mockResolvedValueOnce("h_workbox");
		const localIdentity = createLocalHostIdentity(() => ({ port: 4011, generation }), localProbe);
		const start = vi.fn(async () => { throw new Error("unused"); });
		const ipc = fakeIpc();
		registerRemotes(ipc.ipcMain, { file, registry: new RemoteRegistry(start), requireAccount: signedIn, localIdentity });
		await expect(ipc.invoke("remotes:list")).resolves.toHaveLength(1);
		generation++;
		await expect(ipc.invoke("remotes:connect", "http://192.0.2.1:1")).rejects.toThrow(/ is offline$/);
		await expect(ipc.invoke("remotes:list")).resolves.toEqual([]);
		await expect(ipc.invoke("remotes:connect", "http://192.0.2.1:1")).rejects.toThrow(/itself/);
		expect(start).not.toHaveBeenCalled();
	});

	it.each(["account", "local identity"])("does not authenticate after %s changes while identity is pending", async (changed) => {
		const file = await tempFile();
		let account = TEST_ACCOUNT;
		let localHostId = "h_this_mac";
		const probe = vi.fn(async () => "online" as const);
		const start = vi.fn(async () => { throw new Error("proxy must not start"); });
		let finish!: (hostId: string) => void;
		const identity = vi.fn(() => new Promise<string>((resolve) => { finish = resolve; }));
		const ipc = fakeIpc();
		registerRemotes(ipc.ipcMain, { file, registry: new RemoteRegistry(start), requireAccount: signedIn, getAccountId: async () => account, localIdentity: async () => localHostId, identity, probe });
		const pending = ipc.invoke("remotes:connect", "http://192.0.2.1:1");
		await vi.waitFor(() => expect(identity).toHaveBeenCalled());
		if (changed === "account") account = "another-account";
		else localHostId = "h_workbox";
		finish("h_workbox");
		await expect(pending).rejects.toThrow(/changed/);
		expect(probe).not.toHaveBeenCalled();
		expect(start).not.toHaveBeenCalled();
	});

	it("lists the current account rather than an account that signed out while local identity was pending", async () => {
		const file = await tempFile();
		let account = TEST_ACCOUNT;
		let finish!: (hostId: string) => void;
		const localIdentity = vi.fn(() => new Promise<string>((resolve) => { finish = resolve; }));
		const ipc = fakeIpc();
		registerRemotes(ipc.ipcMain, { file, registry: new RemoteRegistry(async () => { throw new Error("unused"); }), requireAccount: signedIn, getAccountId: async () => account, localIdentity });
		const pending = ipc.invoke("remotes:list");
		await vi.waitFor(() => expect(localIdentity).toHaveBeenCalled());
		account = "another-account";
		finish("h_this_mac");
		await expect(pending).resolves.toEqual([]);
	});

	it("does not start a proxy after the account changes during the initial local identity request", async () => {
		const file = await tempFile();
		let account = TEST_ACCOUNT;
		let finish!: (hostId: string) => void;
		const localIdentity = vi.fn().mockImplementationOnce(() => new Promise<string>((resolve) => { finish = resolve; })).mockResolvedValue("h_this_mac");
		const probe = vi.fn(async () => "online" as const);
		const start = vi.fn(async () => { throw new Error("proxy must not start"); });
		const ipc = fakeIpc();
		registerRemotes(ipc.ipcMain, { file, registry: new RemoteRegistry(start), requireAccount: signedIn, getAccountId: async () => account, localIdentity, identity: async () => "h_workbox", probe });
		const pending = ipc.invoke("remotes:connect", "http://192.0.2.1:1");
		await vi.waitFor(() => expect(localIdentity).toHaveBeenCalled());
		account = "another-account";
		finish("h_this_mac");
		await expect(pending).rejects.toThrow(/Account changed/);
		expect(probe).not.toHaveBeenCalled();
		expect(start).not.toHaveBeenCalled();
	});

	it("serves saved hosts without an AO Cloud account and keeps cloud sync behind sign-in", async () => {
		const file = await tempFile();
		const registry = new RemoteRegistry(async () => { throw new Error("proxy must not start"); });
		const ipc = fakeIpc();
		const requireAccount = vi.fn(async () => { throw new Error("Sign in to AO Cloud to use remote hosts."); });
		registerRemotes(ipc.ipcMain, { file, registry, requireAccount, getAccountId: async () => "" });
		await expect(ipc.invoke("remotes:list")).resolves.toEqual([{ hostId: "h_workbox", label: "workbox", url: "http://192.0.2.1:1", allowInsecureHTTP: false }]);
		await expect(ipc.invoke("remotes:importAccountHost", TEST_ACCOUNT, { hostId: "h_x", label: "x", url: "https://x.example", password: "a".repeat(64) })).rejects.toThrow(/Sign in to AO Cloud/);
		await expect(ipc.invoke("remotes:issueAccountToken", "http://192.0.2.1:1")).rejects.toThrow(/Sign in to AO Cloud/);
		expect(requireAccount).toHaveBeenCalledTimes(2);
	});

	it("merges hosts the daemon found on the tailnet and drops this machine's own installs", async () => {
		const file = await tempFile();
		await writeFile(file, JSON.stringify({ remotes: [
			{ hostId: "h_workbox", label: "Workbox (LAN)", url: "http://192.0.2.1:1", password: "old" },
			{ hostId: "h_old_self", label: "Linux", url: "http://192.0.2.9:3011", password: "self" },
			{ hostId: "h_tunnel", label: "tunnel-box", url: "https://tunnel.example", password: "t" },
		] }));
		const discovery = {
			available: true, localHostIds: ["h_this_mac", "h_old_self"],
			hosts: [
				{ hostId: "h_z600", label: "hpz600", name: "hpz600", dnsName: "hpz600.tail.ts.net", address: "100.106.144.10", port: 3011, url: "http://100.106.144.10:3011", online: true, hidden: false },
				{ hostId: "h_workbox", label: "petars-imac", name: "petars-imac", dnsName: "", address: "100.100.160.17", port: 3011, url: "http://100.100.160.17:3011", online: false, hidden: false },
				{ hostId: "h_hidden", label: "hidden", name: "hidden", dnsName: "", address: "100.1.1.1", port: 3011, url: "http://100.1.1.1:3011", online: true, hidden: true },
			],
		};
		const updateTailnet = vi.fn(async () => undefined);
		const ipc = fakeIpc();
		registerRemotes(ipc.ipcMain, { file, registry: new RemoteRegistry(async () => { throw new Error("unused"); }), requireAccount: signedIn, getAccountId: async () => "", daemonBaseUrl: () => "http://127.0.0.1:3002", discover: async () => discovery, updateTailnet });
		await expect(ipc.invoke("remotes:list")).resolves.toEqual([
			{ hostId: "h_workbox", label: "Workbox (LAN)", url: "http://100.100.160.17:3011", tailscale: true, online: false, allowInsecureHTTP: false },
			{ hostId: "h_tunnel", label: "tunnel-box", url: "https://tunnel.example", allowInsecureHTTP: false },
			{ hostId: "h_z600", label: "hpz600", url: "http://100.106.144.10:3011", tailscale: true, online: true, allowInsecureHTTP: false },
		]);
		expect(JSON.parse(await readFile(file, "utf8")).remotes).toEqual([
			{ hostId: "h_workbox", label: "Workbox (LAN)", url: "http://100.100.160.17:3011", password: "old", tailscale: true },
			{ hostId: "h_tunnel", label: "tunnel-box", url: "https://tunnel.example", password: "t" },
			{ hostId: "h_z600", label: "hpz600", url: "http://100.106.144.10:3011", password: "", tailscale: true },
		]);
		await expect(ipc.invoke("remotes:tailnet")).resolves.toEqual({
			available: true, error: undefined, selfLogin: undefined,
			hidden: [{ hostId: "h_hidden", label: "hidden", name: "hidden", address: "100.1.1.1", url: "http://100.1.1.1:3011", online: true }],
		});
		await ipc.invoke("remotes:hide", "h_z600");
		expect(updateTailnet).toHaveBeenCalledWith("http://127.0.0.1:3002", "h_z600", { hidden: true });
		expect(JSON.parse(await readFile(file, "utf8")).remotes.map((entry: { hostId: string }) => entry.hostId)).toEqual(["h_workbox", "h_tunnel"]);
		await ipc.invoke("remotes:update", "http://100.100.160.17:3011", { label: "IMac" });
		expect(updateTailnet).toHaveBeenCalledWith("http://127.0.0.1:3002", "h_workbox", { label: "IMac" });
		await expect(ipc.invoke("remotes:update", "http://100.100.160.17:3011", { url: "http://192.0.2.1:1" })).rejects.toThrow(/Tailscale address/);
	});

	it.each(["hide", "unhide"])("discards discovery that overlaps %s", async (action) => {
		const file = await tempFile();
		const host = { hostId: "h_peer", label: "Peer", name: "peer", dnsName: "", address: "100.1.1.2", port: 3011, url: "http://100.1.1.2:3011", online: true, hidden: action === "unhide" };
		const snapshot = () => ({ available: true, localHostIds: [], hosts: [{ ...host }] });
		const discover = vi.fn(async () => snapshot());
		const ipc = fakeIpc();
		registerRemotes(ipc.ipcMain, {
			file, registry: new RemoteRegistry(async () => { throw new Error("unused"); }),
			requireAccount: signedIn, discover,
			updateTailnet: async (_base, _id, changes) => { host.hidden = changes.hidden!; },
		});
		await ipc.invoke("remotes:list");
		const stale = snapshot();
		let finish!: () => void;
		discover.mockImplementationOnce(() => new Promise((resolve) => { finish = () => resolve(stale); }));
		const listing = ipc.invoke("remotes:list");
		await vi.waitFor(() => expect(discover).toHaveBeenCalledTimes(2));
		await ipc.invoke(`remotes:${action}`, "h_peer");
		finish();
		await listing;
		const entries = JSON.parse(await readFile(file, "utf8")).remotes;
		expect(entries.some((entry: { hostId: string }) => entry.hostId === "h_peer")).toBe(false);
		await expect(ipc.invoke("remotes:tailnet")).resolves.toMatchObject({ hidden: host.hidden ? [expect.objectContaining({ hostId: "h_peer" })] : [] });
		expect(discover).toHaveBeenCalledTimes(3);
		const fresh = await ipc.invoke("remotes:list") as Array<{ hostId: string }>;
		expect(fresh.some((entry) => entry.hostId === "h_peer")).toBe(!host.hidden);
	});

	it("connects a tailnet host with its identity only, never a bearer", async () => {
		const requests: Array<{ path: string; authorization: string | undefined }> = [];
		const server = createServer((request, response) => {
			requests.push({ path: request.url ?? "", authorization: request.headers.authorization });
			response.setHeader("content-type", "application/json");
			if (request.headers.authorization) { response.statusCode = 403; response.end("{}"); return; }
			response.end(JSON.stringify(request.url === "/api/v1/identity"
				? { hostId: "h_z600", apiVersion: 1 }
				: { status: "ok", service: "agent-orchestrator-daemon", pid: 1234 }));
		});
		await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
		try {
			const address = server.address();
			if (!address || typeof address === "string") throw new Error("missing test port");
			const url = `http://127.0.0.1:${address.port}`;
			const file = await tempFile();
			await writeFile(file, JSON.stringify({ remotes: [{ label: "hpz600", url, password: "", hostId: "h_z600", tailscale: true }] }));
			const start = vi.fn(async () => ({ base: "http://127.0.0.1:4000", close: async () => undefined, previewUrl: () => "", resolvePreviewUrl: () => "" }));
			const ipc = fakeIpc();
			registerRemotes(ipc.ipcMain, { file, registry: new RemoteRegistry(start), requireAccount: signedIn, getAccountId: async () => "" });
			await expect(ipc.invoke("remotes:connect", url)).resolves.toMatchObject({ hostId: "h_z600", base: "http://127.0.0.1:4000" });
			expect(requests.every((request) => request.authorization === undefined)).toBe(true);
			expect(requests.map((request) => request.path)).toEqual(["/api/v1/identity", "/healthz"]);
		} finally {
			await new Promise<void>((resolve) => server.close(() => resolve()));
		}
	});

	it("saves the observed host ID and authenticates only after the identity probe", async () => {
		const requests: Array<{ path: string; authorization: string | undefined }> = [];
		const server = createServer((request, response) => {
			requests.push({ path: request.url ?? "", authorization: request.headers.authorization });
			response.setHeader("content-type", "application/json");
			response.end(JSON.stringify(request.url === "/api/v1/identity"
				? { hostId: "h_new", apiVersion: 1 }
				: { status: "ok", service: "agent-orchestrator-daemon", pid: 1234 }));
		});
		await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
		try {
			const address = server.address();
			if (!address || typeof address === "string") throw new Error("missing test port");
			const url = `http://127.0.0.1:${address.port}`;
			const file = await tempFile();
			const ipc = fakeIpc();
			registerRemotes(ipc.ipcMain, {
				file,
				requireAccount: signedIn,
				registry: new RemoteRegistry(async () => { throw new Error("unused"); }),
			});
			await expect(ipc.invoke("remotes:add", { label: "mini", url, password: "secret" })).resolves.toBe("online");
			const saved = JSON.parse(await readFile(file, "utf8")).remotes as Array<{ url: string; hostId: string }>;
			expect(saved.find((entry) => entry.url === url)?.hostId).toBe("h_new");
			expect(requests[0]).toEqual({ path: "/api/v1/identity", authorization: undefined });
			expect(requests.filter((request) => request.authorization)).toEqual([
				{ path: "/healthz", authorization: "Bearer secret" },
			]);
		} finally {
			await new Promise<void>((resolve) => server.close(() => resolve()));
		}
	});

	it("refuses a changed host before sending the saved password", async () => {
		let authenticated = false;
		const server = createServer((request, response) => {
			if (request.headers.authorization) authenticated = true;
			response.setHeader("content-type", "application/json");
			response.end(JSON.stringify(request.url === "/api/v1/identity"
				? { hostId: "h_other", apiVersion: 1 }
				: { status: "ok", service: "agent-orchestrator-daemon", pid: 1234 }));
		});
		await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
		try {
			const address = server.address();
			if (!address || typeof address === "string") throw new Error("missing test port");
			const url = `http://127.0.0.1:${address.port}`;
			const file = await tempFile();
			await writeFile(file, JSON.stringify({ remotes: [{ label: "workbox", url, password: "secret", hostId: "h_expected", accountUserId: TEST_ACCOUNT }] }));
			const ipc = fakeIpc();
			registerRemotes(ipc.ipcMain, {
				file,
				requireAccount: signedIn,
				registry: new RemoteRegistry(async () => { throw new Error("proxy must not start"); }),
			});
			await expect(ipc.invoke("remotes:connect", url)).rejects.toThrow(/identity|host/i);
			expect(authenticated).toBe(false);
		} finally {
			await new Promise<void>((resolve) => server.close(() => resolve()));
		}
	});

	it("reports an incompatible host before sending or saving its credential", async () => {
		let authenticated = false;
		const server = createServer((request, response) => {
			if (request.headers.authorization) authenticated = true;
			response.setHeader("content-type", "application/json");
			response.end(JSON.stringify({ hostId: "h_new", apiVersion: 2 }));
		});
		await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
		try {
			const address = server.address();
			if (!address || typeof address === "string") throw new Error("missing test port");
			const url = `http://127.0.0.1:${address.port}`;
			const file = await tempFile();
			const ipc = fakeIpc();
			registerRemotes(ipc.ipcMain, {
				file,
				requireAccount: signedIn,
				registry: new RemoteRegistry(async () => { throw new Error("proxy must not start"); }),
			});
			await expect(ipc.invoke("remotes:add", { label: "new", url, password: "secret" })).resolves.toBe("incompatible");
			expect(authenticated).toBe(false);
			expect((JSON.parse(await readFile(file, "utf8")).remotes as Array<{ url: string }>).some((host) => host.url === url)).toBe(false);
		} finally {
			await new Promise<void>((resolve) => server.close(() => resolve()));
		}
	});

	it("keeps the real host connected when another saved host points at its URL", async () => {
		const file = await tempFile();
		const url = "http://127.0.0.1:9123";
		await writeFile(file, JSON.stringify({ remotes: [
			{ hostId: "h_a", label: "A", url, password: "a", accountUserId: TEST_ACCOUNT },
			{ hostId: "h_b", label: "B", url, password: "b", accountUserId: TEST_ACCOUNT },
		] }));
		const closed = vi.fn().mockResolvedValue(undefined);
		const registry = new RemoteRegistry(async () => ({ base: "http://127.0.0.1:5000/token", previewUrl: (_sessionId, sourceUrl) => sourceUrl, resolvePreviewUrl: (_sessionId, viewedUrl) => viewedUrl, close: closed }));
		await registry.connect({ hostId: "h_b", label: "B", url, password: "b" });
		const ipc = fakeIpc();
		registerRemotes(ipc.ipcMain, { file, registry, requireAccount: signedIn, identity: async () => "h_b", probe: async () => "online" });

		await expect(ipc.invoke("remotes:connect", url, "h_a")).rejects.toThrow(/identity changed/);
		await expect(ipc.invoke("remotes:connect", url, "h_b")).resolves.toMatchObject({ hostId: "h_b" });
		expect(closed).not.toHaveBeenCalled();
	});

	it("reports an unreachable new address as offline without changing the saved host", async () => {
		const file = await tempFile();
		const ipc = fakeIpc();
		registerRemotes(ipc.ipcMain, {
			file,
			requireAccount: signedIn,
			registry: new RemoteRegistry(async () => { throw new Error("proxy must not start"); }),
			identity: async () => { throw new TypeError("fetch failed"); },
		});

		await expect(ipc.invoke("remotes:update", "http://192.0.2.1:1", { url: "https://new.trycloudflare.com" })).resolves.toBe("offline");
		expect((JSON.parse(await readFile(file, "utf8")).remotes as Array<{ url: string }>)[0].url).toBe("http://192.0.2.1:1");
	});

	it("lists hosts without their passwords", async () => {
		const ipc = fakeIpc();
		registerRemotes(ipc.ipcMain, { file: await tempFile(), registry: new RemoteRegistry(async () => { throw new Error("unused"); }), requireAccount: signedIn });
		await expect(ipc.invoke("remotes:list")).resolves.toEqual([{ hostId: "h_workbox", label: "workbox", url: "http://192.0.2.1:1", allowInsecureHTTP: false }]);
	});

	it("saves a new host only after it answers as a daemon", async () => {
		const ipc = fakeIpc();
		const file = await tempFile();
		const probe = vi.fn().mockResolvedValueOnce("offline" as const).mockResolvedValueOnce("online" as const);
		registerRemotes(ipc.ipcMain, {
			file,
			requireAccount: signedIn,
			registry: new RemoteRegistry(async () => { throw new Error("unused"); }),
			probe,
			identity: async () => "h_mini",
		});
		const mini = { label: "mini", url: "http://192.0.2.9:9", password: "m" };

		await expect(ipc.invoke("remotes:add", mini)).resolves.toBe("offline");
		expect(JSON.parse(await readFile(file, "utf8")).remotes).toHaveLength(1);

		await expect(ipc.invoke("remotes:add", mini)).resolves.toBe("online");
		expect(JSON.parse(await readFile(file, "utf8")).remotes).toHaveLength(2);
		expect(JSON.parse(await readFile(file, "utf8")).remotes[1].hostId).toBe("h_mini");
	});

	it("writes the plain-http opt-in without probing or reconnecting, so revoking it works against an unauthorized host", async () => {
		const file = await tempFile();
		await writeFile(file, JSON.stringify({ remotes: [
			{ hostId: "h_lan", label: "LAN", url: "http://192.168.0.164:3011", password: "rotated", accountUserId: "user-a", allowInsecureHTTP: true },
			{ hostId: "h_other", label: "Other", url: "http://192.168.0.165:3011", password: "pw", accountUserId: "user-b" },
		] }));
		const probe = vi.fn(async () => "unauthorized" as const);
		const registry = new RemoteRegistry(async () => { throw new Error("unused"); });
		const disconnect = vi.spyOn(registry, "disconnect");
		const ipc = fakeIpc();
		registerRemotes(ipc.ipcMain, { file, registry, requireAccount: signedIn, getAccountId: async () => "user-a", identity: async () => "h_workbox", probe, localIdentity: async () => "h_workbox" });

		await expect(ipc.invoke("remotes:setAllowInsecureHTTP", "http://192.168.0.164:3011", false)).resolves.toBeUndefined();
		expect(probe).not.toHaveBeenCalled();
		expect(disconnect).not.toHaveBeenCalled();
		const saved = JSON.parse(await readFile(file, "utf8")) as { remotes: Array<{ url: string; allowInsecureHTTP?: boolean; password: string }> };
		expect(saved.remotes[0]).toMatchObject({ url: "http://192.168.0.164:3011", allowInsecureHTTP: false, password: "rotated" });
		await expect(ipc.invoke("remotes:setAllowInsecureHTTP", "http://192.168.0.165:3011", true)).rejects.toThrow(/does not belong/);
	});

	it("drops the proxy of a removed host", async () => {
		const ipc = fakeIpc();
		const closed = vi.fn().mockResolvedValue(undefined);
		const registry = new RemoteRegistry(async () => ({ base: "http://127.0.0.1:7654/token", previewUrl: (_sessionId, sourceUrl) => sourceUrl, resolvePreviewUrl: (_sessionId, viewedUrl) => viewedUrl, close: closed }));
		await registry.connect({ hostId: "h_workbox", label: "workbox", url: "http://192.0.2.1:1", password: "old" });
		registerRemotes(ipc.ipcMain, { file: await tempFile(), registry, requireAccount: signedIn });
		await ipc.invoke("remotes:remove", "http://192.0.2.1:1");
		expect(closed).toHaveBeenCalledOnce();
	});

	it("imports only the signed-in account's hosts and prunes removed imports", async () => {
		const file = await tempFile();
		await writeFile(file, JSON.stringify({ remotes: [
			{ hostId: "h_manual", label: "Manual", url: "https://manual.example", password: "password", accountUserId: "user-a" },
			{ hostId: "h_other", label: "Other", url: "https://other.example", password: "password", accountUserId: "user-b" },
		] }));
		let account = "user-a";
		const ipc = fakeIpc();
		registerRemotes(ipc.ipcMain, {
			file, registry: new RemoteRegistry(async () => { throw new Error("unused"); }),
			requireAccount: signedIn, getAccountId: async () => account,
		});
		await ipc.invoke("remotes:importAccountHost", "user-a", { hostId: "h_cloud", label: "Cloud", url: "https://cloud.example", password: "a".repeat(64) });
		await expect(ipc.invoke("remotes:list")).resolves.toEqual([
			{ hostId: "h_manual", label: "Manual", url: "https://manual.example", allowInsecureHTTP: false },
			{ hostId: "h_cloud", label: "Cloud", url: "https://cloud.example", allowInsecureHTTP: false },
		]);
		await ipc.invoke("remotes:pruneAccountHosts", "user-a", []);
		await expect(ipc.invoke("remotes:list")).resolves.toEqual([
			{ hostId: "h_manual", label: "Manual", url: "https://manual.example", allowInsecureHTTP: false },
		]);
		account = "user-b";
		await expect(ipc.invoke("remotes:importAccountHost", "user-a", { hostId: "h_leak", label: "Leak", url: "https://leak.example", password: "a".repeat(64) })).rejects.toThrow(/Invalid account/);
		await expect(ipc.invoke("remotes:list")).resolves.toEqual([
			{ hostId: "h_other", label: "Other", url: "https://other.example", allowInsecureHTTP: false },
		]);
	});

	it("preserves and shows a legacy pairing when its host is discovered in the account", async () => {
		const file = await tempFile();
		await writeFile(file, JSON.stringify({ remotes: [
			{ hostId: "h_legacy", label: "Old", url: "https://old.example", password: "pairing-password" },
		] }));
		const ipc = fakeIpc();
		registerRemotes(ipc.ipcMain, {
			file, registry: new RemoteRegistry(async () => { throw new Error("unused"); }),
			requireAccount: signedIn, getAccountId: async () => "user-a",
		});

		await ipc.invoke("remotes:importAccountHost", "user-a", {
			hostId: "h_legacy", label: "Discovered", url: "https://new.example", password: "a".repeat(64),
		});
		await expect(ipc.invoke("remotes:list")).resolves.toEqual([
			{ hostId: "h_legacy", label: "Discovered", url: "https://new.example", allowInsecureHTTP: false },
		]);
		const saved = JSON.parse(await readFile(file, "utf8")) as { remotes: Array<{ password: string }> };
		expect(saved.remotes[0]?.password).toBe("pairing-password");
	});
});

describe("account host address", () => {
	it.each([
		["http://100.100.160.17:57451", "http://100.100.160.17:57451"],
		["http://imac.tail1234.ts.net:57451/", "http://imac.tail1234.ts.net:57451"],
		["http://[fd7a:115c:a1e0::1]:3011", "http://[fd7a:115c:a1e0::1]:3011"],
	])("accepts the Tailscale address %s", (raw, want) => {
		expect(tailnetAccountUrl(raw)).toBe(want);
	});

	it.each([undefined, "", "http://192.168.0.126:57451", "http://10.0.0.5:3011", "https://abc.trycloudflare.com", "http://100.128.0.1:3011"])("refuses %s instead of falling back to a LAN address", (raw) => {
		expect(() => tailnetAccountUrl(raw)).toThrow(NO_TAILSCALE_ADDRESS);
	});

	it.each([
		["http://100.100.160.17:57451", { url: "http://100.100.160.17:57451" }],
		[undefined, null],
	])("links the account by the host's Tailscale address (%s)", async (reported, expected) => {
		const file = await tempFile();
		const token = "a".repeat(64);
		const fetchMock = vi.fn(async () => new Response(JSON.stringify({ hostId: "h_workbox", token, url: reported }), { status: 200 }));
		vi.stubGlobal("fetch", fetchMock);
		try {
			const ipc = fakeIpc();
			registerRemotes(ipc.ipcMain, { file, registry: new RemoteRegistry(async () => { throw new Error("unused"); }), requireAccount: signedIn, identity: async () => "h_workbox" });
			const issued = ipc.invoke("remotes:issueAccountToken", "http://192.0.2.1:1");
			if (expected) await expect(issued).resolves.toEqual({ token, ...expected });
			else await expect(issued).rejects.toThrow(NO_TAILSCALE_ADDRESS);
			expect(fetchMock).toHaveBeenCalledOnce();
		} finally {
			vi.unstubAllGlobals();
		}
	});
});
