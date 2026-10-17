import { chmodSync, mkdirSync, mkdtempSync, readdirSync, readFileSync, rmSync, statSync, writeFileSync } from "node:fs";
import { createServer, type IncomingHttpHeaders, type Server } from "node:http";
import type { AddressInfo } from "node:net";
import { tmpdir } from "node:os";
import path from "node:path";
import request from "supertest";
import { afterEach, beforeAll, beforeEach, describe, expect, it } from "vitest";
import { createHostsApi } from "../src/hosts-api.ts";
import { createHostsMonitor, type HostsMonitor } from "../src/hosts-probe.ts";
import { HostsFilePermissionError, HostsStore, normalizeHostUrl, readHubEntry } from "../src/hosts-store.ts";
import type { LogFields } from "../src/log.ts";
import type { AuditEntry, AuditEvent, AuditLog } from "../src/audit.ts";
import { hashPassword } from "../src/password.ts";
import { createBff } from "../src/server.ts";

const HOST = "hub.test";
const ORIGIN = `http://${HOST}`;
const OPERATOR_PASSWORD = "operator-password";

// FakeHost answers like a daemon's authenticated remote-host listener:
// GET /api/v1/identity without auth, everything else behind the bearer.
type FakeHost = {
	url: string;
	hostId: string;
	password: string;
	apiVersion: number;
	seen: { path: string; headers: IncomingHttpHeaders }[];
	close: () => Promise<void>;
};

async function fakeHost(hostId: string, password: string): Promise<FakeHost> {
	const host: FakeHost = { url: "", hostId, password, apiVersion: 1, seen: [], close: async () => {} };
	const server: Server = createServer((req, res) => {
		host.seen.push({ path: req.url ?? "", headers: req.headers });
		const json = (status: number, body: unknown) => res.writeHead(status, { "Content-Type": "application/json" }).end(JSON.stringify(body));
		if (req.url === "/api/v1/identity") return json(200, { hostId: host.hostId, apiVersion: host.apiVersion });
		if (req.headers.authorization !== `Bearer ${host.password}`) return json(401, { error: "unauthorized" });
		const expected = req.headers["x-ao-expected-host-id"];
		if (expected && expected !== host.hostId) return json(421, { code: "HOST_ID_MISMATCH" });
		if (req.url === "/api/v1/projects") return json(200, { projects: [] });
		return json(404, {});
	});
	await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
	host.url = `http://127.0.0.1:${(server.address() as AddressInfo).port}`;
	host.close = () => new Promise((resolve) => server.close(() => resolve()));
	return host;
}

let passwordHash: string;
beforeAll(async () => {
	passwordHash = await hashPassword(OPERATOR_PASSWORD, { ln: 10, r: 8, p: 1 });
});

let dir: string;
let spaDir: string;
let hubConfigPath: string;
let hosts: FakeHost[];
beforeEach(() => {
	dir = mkdtempSync(path.join(tmpdir(), "hub-bff-hosts-"));
	spaDir = path.join(dir, "spa");
	mkdirSync(spaDir);
	writeFileSync(path.join(spaDir, "index.html"), "<!doctype html>");
	hubConfigPath = path.join(dir, "ao", "mobile", "config.json");
	hosts = [];
});
afterEach(async () => {
	await Promise.all(hosts.map((h) => h.close()));
	rmSync(dir, { recursive: true, force: true });
});

async function startHost(hostId: string, password: string): Promise<FakeHost> {
	const host = await fakeHost(hostId, password);
	hosts.push(host);
	return host;
}

function writeHubConfig(config: object): void {
	mkdirSync(path.dirname(hubConfigPath), { recursive: true });
	writeFileSync(hubConfigPath, JSON.stringify(config), { mode: 0o600 });
}

type Harness = { server: Server; store: HostsStore; monitor: HostsMonitor; logs: string[]; auditEntries: AuditEntry[]; cookie: string };

async function harness(): Promise<Harness> {
	const logs: string[] = [];
	const auditEntries: AuditEntry[] = [];
	const audit: AuditLog = {
		async record(event: AuditEvent, subjectId?: string) { auditEntries.push({ ts: new Date().toISOString(), event, ...(subjectId ? { subjectId } : {}) }); },
		async list(limit: number) { return auditEntries.slice(-limit); },
	};
	const log = (level: string, msg: string, fields?: LogFields) => logs.push(JSON.stringify({ level, msg, ...fields }));
	const store = await HostsStore.open(path.join(dir, "hub"), { allowLoopback: true });
	const monitor = createHostsMonitor({ store, hubConfigPath, log });
	await monitor.syncHub();
	const server = createBff({
		spaDir,
		passwordHash,
		log,
		hosts: store,
		hostsApi: createHostsApi({ store, monitor, log, audit, allowLoopback: true }),
		audit,
		verify: async (candidate) => candidate === OPERATOR_PASSWORD,
	});
	const res = await request(server).post("/login").set("Host", HOST).set("Origin", ORIGIN).send({ password: OPERATOR_PASSWORD });
	expect(res.status).toBe(204);
	const cookie = (res.headers["set-cookie"] as unknown as string[])[0].split(";")[0];
	return { server, store, monitor, logs, auditEntries, cookie };
}

function post(h: Harness, url: string, body?: object) {
	const req = request(h.server).post(url).set("Host", HOST).set("Origin", ORIGIN).set("Cookie", h.cookie);
	return body ? req.send(body) : req;
}

function del(h: Harness, url: string) {
	return request(h.server).delete(url).set("Host", HOST).set("Origin", ORIGIN).set("Cookie", h.cookie);
}

function list(h: Harness) {
	return request(h.server).get("/hub/hosts").set("Cookie", h.cookie);
}

describe("hosts store", () => {
	it("accepts only http(s) URLs without credentials, query, or fragment", () => {
		expect(normalizeHostUrl("https://worker-1.up.railway.app/")).toBe("https://worker-1.up.railway.app");
		expect(normalizeHostUrl("http://worker-1.railway.internal:3011")).toBe("http://worker-1.railway.internal:3011");
		for (const bad of ["ftp://w.example", "file:///etc/passwd", "worker-1:3011", "https://u:p@w.example", "https://w.example/?a=1", "https://w.example/#x", "https://w.example/ao", ""]) {
			expect(() => normalizeHostUrl(bad), bad).toThrow();
		}
	});

	it("refuses loopback addresses for manual hosts, so the daemon's own listeners stay out of reach", () => {
		for (const bad of ["http://127.0.0.1:3001", "http://localhost:3011", "http://[::1]:3001", "http://0.0.0.0:3001", "http://[::ffff:127.0.0.1]:3001", "http://127.8.0.1", "http://localhost.:3011", "http://foo.localhost.:3011", "http://localhost.:3001", "http://LOCALHOST..:3001", "http://127.0.0.1.:3001"]) {
			expect(() => normalizeHostUrl(bad), bad).toThrow(/loopback/);
		}
	});

	it("writes hosts.json atomically with mode 0600 and reloads it", async () => {
		const store = await HostsStore.open(path.join(dir, "hub"));
		await store.add({ label: "worker-1", url: "http://worker-1.railway.internal:3011", password: "secret", hostId: "h_1" });
		const file = path.join(dir, "hub", "hosts.json");
		expect(statSync(file).mode & 0o777).toBe(0o600);
		const mirrorFile = path.join(dir, "hub", "remotes.json");
		expect(statSync(mirrorFile).mode & 0o777).toBe(0o600);
		expect(readdirSync(path.join(dir, "hub")).sort()).toEqual(["hosts.json", "remotes.json"]);
		expect(JSON.parse(readFileSync(mirrorFile, "utf8"))).toEqual({ remotes: [
			{ label: "worker-1", url: "http://worker-1.railway.internal:3011", password: "secret", hostId: "h_1" },
		] });
		const saved = JSON.parse(readFileSync(file, "utf8"));
		expect(saved).toEqual([
			expect.objectContaining({ label: "worker-1", url: "http://worker-1.railway.internal:3011", password: "secret", hostId: "h_1", source: "manual", status: "unknown" }),
		]);
		expect(saved[0].createdAt).toMatch(/^\d{4}-/);
		const reopened = await HostsStore.open(path.join(dir, "hub"));
		expect(reopened.getHost("h_1")).toEqual({ url: "http://worker-1.railway.internal:3011", hostId: "h_1", bearer: "secret" });
		expect(reopened.list()[0]).not.toHaveProperty("password");
	});

	it("mirrors only confirmed, active non-hub hosts through add, identity changes, and remove", async () => {
		const store = await HostsStore.open(path.join(dir, "hub"));
		const mirror = () => JSON.parse(readFileSync(path.join(dir, "hub", "remotes.json"), "utf8"));
		await store.upsertHub({ url: "http://127.0.0.1:3011", password: "hub-secret" });
		await store.createWorker({ id: "worker-record", label: "worker", password: "worker-secret" });
		expect(mirror()).toEqual({ remotes: [] });
		await store.updateWorker("worker-record", { url: "http://worker.example:3011", hostId: "worker-id", status: "online" });
		expect(mirror()).toEqual({ remotes: [
			{ label: "worker", url: "http://worker.example:3011", password: "worker-secret", hostId: "worker-id" },
		] });
		await store.recordProbe("worker-id", { status: "online", hostId: "worker-id" });
		expect(mirror().remotes[0].hostId).toBe("worker-id");
		await store.updateWorker("worker-record", { desired: "removed" });
		expect(mirror()).toEqual({ remotes: [] });
		await store.removeWorker("worker-record");
		expect(mirror()).toEqual({ remotes: [] });
	});

	it("keeps labels unique, case-insensitively, and reserves the hub label", async () => {
		const store = await HostsStore.open(path.join(dir, "hub"));
		await store.add({ label: "worker-1", url: "http://a.example", password: "p", hostId: "h_1" });
		await expect(store.add({ label: "Worker-1", url: "http://b.example", password: "p", hostId: "h_2" })).rejects.toMatchObject({ code: "label_taken" });
		await expect(store.add({ label: "hub", url: "http://b.example", password: "p", hostId: "h_2" })).rejects.toMatchObject({ code: "label_taken" });
		await expect(store.add({ label: "worker-2", url: "http://b.example", password: "p", hostId: "h_1" })).rejects.toMatchObject({ code: "host_exists" });
	});

	it("serializes concurrent writes", async () => {
		const store = await HostsStore.open(path.join(dir, "hub"));
		await Promise.all(
			Array.from({ length: 5 }, (_, i) => store.add({ label: `w${i}`, url: `http://w${i}.example`, password: "p", hostId: `h_${i}` })),
		);
		const reopened = await HostsStore.open(path.join(dir, "hub"));
		expect(reopened.list().map((h) => h.label).sort()).toEqual(["w0", "w1", "w2", "w3", "w4"]);
	});

	it("refuses a hosts.json readable by group or others", async () => {
		mkdirSync(path.join(dir, "hub"));
		const file = path.join(dir, "hub", "hosts.json");
		writeFileSync(file, "[]");
		chmodSync(file, 0o644);
		await expect(HostsStore.open(path.join(dir, "hub"))).rejects.toBeInstanceOf(HostsFilePermissionError);
	});

	it("derives the hub entry from mobile/config.json, preferring the account token", async () => {
		expect(await readHubEntry(hubConfigPath)).toBeUndefined();
		writeHubConfig({ enabled: false, password: "pw", lastPort: 3011 });
		expect(await readHubEntry(hubConfigPath)).toBeUndefined();
		writeHubConfig({ enabled: true, password: "pw", loopbackOnly: true, noPublicTunnel: true });
		expect(await readHubEntry(hubConfigPath)).toEqual({ url: "http://127.0.0.1:3011", password: "pw", loopbackOnly: true });
		writeHubConfig({ enabled: true, password: "pw", accountToken: "tok", lastPort: 4011 });
		expect(await readHubEntry(hubConfigPath)).toEqual({ url: "http://127.0.0.1:4011", password: "tok", loopbackOnly: false });
	});

	it("never targets the daemon's unauthenticated loopback port 3001", async () => {
		writeHubConfig({ enabled: true, password: "pw", lastPort: 3001 });
		await expect(readHubEntry(hubConfigPath)).rejects.toThrow(/unauthenticated/);
	});
});

describe("/hub/hosts API", () => {
	it("requires a session", async () => {
		const h = await harness();
		expect((await request(h.server).get("/hub/hosts")).status).toBe(401);
		const res = await request(h.server).post("/hub/hosts").set("Host", HOST).set("Origin", ORIGIN).send({ label: "w", url: "http://w.example", password: "p" });
		expect(res.status).toBe(401);
	});

	it("refuses a cross-origin add", async () => {
		const h = await harness();
		const res = await request(h.server).post("/hub/hosts").set("Host", HOST).set("Origin", "https://evil.example").set("Cookie", h.cookie).send({});
		expect(res.status).toBe(403);
	});

	it("adds a host after probing its identity and checking the bearer, and never returns the password", async () => {
		const worker = await startHost("h_worker", "worker-secret");
		const h = await harness();
		const res = await post(h, "/hub/hosts", { label: "worker-1", url: worker.url, password: "worker-secret" });
		expect(res.status).toBe(201);
		expect(res.body.host).toMatchObject({ id: "h_worker", hostId: "h_worker", label: "worker-1", url: worker.url, source: "manual", status: "online", removable: true });
		expect(JSON.stringify(res.body)).not.toContain("worker-secret");

		const identity = worker.seen.find((r) => r.path === "/api/v1/identity");
		expect(identity?.headers.authorization).toBeUndefined();
		const auth = worker.seen.find((r) => r.path === "/api/v1/projects");
		expect(auth?.headers.authorization).toBe("Bearer worker-secret");
		expect(auth?.headers["x-ao-expected-host-id"]).toBe("h_worker");

		const listed = await list(h);
		expect(listed.status).toBe(200);
		expect(listed.body.hosts.map((x: { id: string }) => x.id)).toEqual(["h_worker"]);
		expect(JSON.stringify(listed.body)).not.toContain("worker-secret");
		expect(h.logs.join("\n")).not.toContain("worker-secret");

		// The reverse proxy's lookup.
		expect(h.store.getHost("h_worker")).toEqual({ url: worker.url, hostId: "h_worker", bearer: "worker-secret" });
	});

	it("proxies /hosts/<id>/api/v1/projects to a host added through the API", async () => {
		const worker = await startHost("h_worker", "worker-secret");
		const h = await harness();
		expect((await request(h.server).get("/hosts/h_worker/api/v1/projects").set("Cookie", h.cookie)).status).toBe(404);
		expect((await post(h, "/hub/hosts", { label: "worker-1", url: worker.url, password: "worker-secret" })).status).toBe(201);
		worker.seen = [];
		const res = await request(h.server).get("/hosts/h_worker/api/v1/projects").set("Cookie", h.cookie);
		expect(res.status).toBe(200);
		expect(res.body).toEqual({ projects: [] });
		expect(worker.seen.map((r) => [r.path, r.headers.authorization, r.headers["x-ao-expected-host-id"]])).toEqual([
			["/api/v1/projects", "Bearer worker-secret", "h_worker"],
		]);
	});

	it("refuses a wrong password with 422, not 401", async () => {
		const worker = await startHost("h_worker", "worker-secret");
		const h = await harness();
		const res = await post(h, "/hub/hosts", { label: "worker-1", url: worker.url, password: "wrong" });
		expect(res.status).toBe(422);
		expect(res.body.error).toBe("host_credential_rejected");
		expect((await list(h)).body.hosts).toEqual([]);
	});

	it("refuses an unreachable host and an unsupported API version", async () => {
		const worker = await startHost("h_worker", "worker-secret");
		const h = await harness();
		await worker.close();
		const unreachable = await post(h, "/hub/hosts", { label: "worker-1", url: worker.url, password: "worker-secret" });
		expect(unreachable.status).toBe(502);
		expect(unreachable.body.error).toBe("host_unreachable");

		const other = await startHost("h_other", "pw");
		other.apiVersion = 2;
		const version = await post(h, "/hub/hosts", { label: "worker-2", url: other.url, password: "pw" });
		expect(version.status).toBe(502);
		expect(version.body.error).toBe("unsupported_api_version");
	});

	it("refuses a host that reports the reserved id hub", async () => {
		const worker = await startHost("hub", "pw");
		const h = await harness();
		const res = await post(h, "/hub/hosts", { label: "fake-hub", url: worker.url, password: "pw" });
		expect(res.status).toBe(502);
		expect(worker.seen.map((r) => r.path)).toEqual(["/api/v1/identity"]);
	});

	it("validates the body and refuses duplicates", async () => {
		const worker = await startHost("h_worker", "worker-secret");
		const h = await harness();
		expect((await post(h, "/hub/hosts", { label: "w", url: "ftp://w.example", password: "p" })).status).toBe(400);
		expect((await post(h, "/hub/hosts", { label: "", url: worker.url, password: "p" })).status).toBe(400);
		expect((await post(h, "/hub/hosts", { label: "w", url: worker.url })).status).toBe(400);
		expect((await post(h, "/hub/hosts").set("Content-Type", "application/json").send("{")).status).toBe(400);
		expect((await post(h, "/hub/hosts").set("Content-Type", "text/plain").send("x")).status).toBe(415);

		expect((await post(h, "/hub/hosts", { label: "worker-1", url: worker.url, password: "worker-secret" })).status).toBe(201);
		const sameLabel = await post(h, "/hub/hosts", { label: "worker-1", url: worker.url, password: "worker-secret" });
		expect(sameLabel.status).toBe(409);
		expect(sameLabel.body.error).toBe("label_taken");
		const sameHost = await post(h, "/hub/hosts", { label: "worker-again", url: worker.url, password: "worker-secret" });
		expect(sameHost.status).toBe(409);
		expect(sameHost.body.error).toBe("host_exists");
		expect(h.auditEntries).toContainEqual(expect.objectContaining({ event: "host.add", subjectId: "h_worker" }));
	});

	it("removes a host from the registry only", async () => {
		const worker = await startHost("h_worker", "worker-secret");
		const h = await harness();
		await post(h, "/hub/hosts", { label: "worker-1", url: worker.url, password: "worker-secret" });
		expect((await del(h, "/hub/hosts/h_worker")).status).toBe(204);
		expect(h.auditEntries).toContainEqual(expect.objectContaining({ event: "host.remove", subjectId: "h_worker" }));
		expect((await list(h)).body.hosts).toEqual([]);
		expect(JSON.parse(readFileSync(path.join(dir, "hub", "hosts.json"), "utf8"))).toEqual([]);
		expect((await del(h, "/hub/hosts/h_worker")).status).toBe(404);
	});

	it("probes a host on demand and reports auth_failed after the password rotates", async () => {
		const worker = await startHost("h_worker", "worker-secret");
		const h = await harness();
		await post(h, "/hub/hosts", { label: "worker-1", url: worker.url, password: "worker-secret" });
		worker.password = "rotated";
		const res = await post(h, "/hub/hosts/h_worker/probe");
		expect(res.status).toBe(200);
		expect(res.body.host).toMatchObject({ id: "h_worker", status: "auth_failed" });
		expect(JSON.stringify(res.body)).not.toContain("worker-secret");
		expect((await post(h, "/hub/hosts/nope/probe")).status).toBe(404);
	});

	it("treats a different host answering at a saved address as unreachable and sends it no bearer", async () => {
		const worker = await startHost("h_worker", "worker-secret");
		const h = await harness();
		await post(h, "/hub/hosts", { label: "worker-1", url: worker.url, password: "worker-secret" });
		worker.hostId = "h_impostor";
		worker.seen = [];
		const res = await post(h, "/hub/hosts/h_worker/probe");
		expect(res.body.host.status).toBe("unreachable");
		expect(worker.seen.map((r) => r.path)).toEqual(["/api/v1/identity"]);
	});

	it("does not follow a redirect with the bearer", async () => {
		const target = await startHost("h_target", "x");
		const redirecting = createServer((req, res) => {
			if (req.url === "/api/v1/identity") res.writeHead(200, { "Content-Type": "application/json" }).end('{"hostId":"h_r","apiVersion":1}');
			else res.writeHead(302, { Location: `${target.url}/api/v1/projects` }).end();
		});
		await new Promise<void>((resolve) => redirecting.listen(0, "127.0.0.1", resolve));
		try {
			const h = await harness();
			const url = `http://127.0.0.1:${(redirecting.address() as AddressInfo).port}`;
			const res = await post(h, "/hub/hosts", { label: "r", url, password: "secret" });
			expect(res.status).toBe(502);
			expect(target.seen).toEqual([]);
		} finally {
			await new Promise((resolve) => redirecting.close(resolve));
		}
	});
});

describe("hub entry", () => {
	it("is created at startup from mobile/config.json, paired by the first probe, and cannot be removed", async () => {
		const hub = await startHost("h_hub", "hub-token");
		writeHubConfig({ enabled: true, password: "pairing", accountToken: "hub-token", lastPort: Number(new URL(hub.url).port), loopbackOnly: true, noPublicTunnel: true });
		const h = await harness();

		const before = (await list(h)).body.hosts;
		expect(before).toEqual([expect.objectContaining({ id: "hub", label: "hub", url: hub.url, source: "hub", status: "unknown", removable: false })]);
		// Not paired yet: the proxy must not get a bearer for it.
		expect(h.store.getHost("hub")).toBeUndefined();

		await h.monitor.pollOnce();
		const after = (await list(h)).body.hosts;
		expect(after[0]).toMatchObject({ id: "h_hub", hostId: "h_hub", status: "online" });
		expect(JSON.stringify(after)).not.toContain("hub-token");
		expect(h.store.getHost("hub")).toEqual({ url: hub.url, hostId: "h_hub", bearer: "hub-token" });
		expect(h.store.getHost("h_hub")).toEqual({ url: hub.url, hostId: "h_hub", bearer: "hub-token" });
		expect(h.store.getHub()).toEqual({ url: hub.url, hostId: "h_hub", bearer: "hub-token" });
		// The proxy now routes /api/* to the hub with its bearer.
		hub.seen = [];
		expect((await request(h.server).get("/api/v1/projects").set("Cookie", h.cookie)).status).toBe(200);
		expect(hub.seen[0].headers.authorization).toBe("Bearer hub-token");

		for (const id of ["hub", "h_hub"]) {
			const res = await del(h, `/hub/hosts/${id}`);
			expect(res.status).toBe(409);
			expect(res.body.error).toBe("hub_not_removable");
		}
		expect((await list(h)).body.hosts).toHaveLength(1);
	});

	it("picks up a rotated hub credential on the next poll", async () => {
		const hub = await startHost("h_hub", "first");
		const port = Number(new URL(hub.url).port);
		writeHubConfig({ enabled: true, password: "first", lastPort: port, loopbackOnly: true });
		const h = await harness();
		await h.monitor.pollOnce();
		expect(h.store.getHost("hub")?.bearer).toBe("first");

		hub.password = "second";
		writeHubConfig({ enabled: true, password: "second", lastPort: port, loopbackOnly: true });
		await h.monitor.pollOnce();
		expect(h.store.getHost("hub")?.bearer).toBe("second");
		expect(h.store.getPublic("hub")?.status).toBe("online");
	});
});

describe("status polling", () => {
	it("probes every host on each interval and records online, auth_failed, and unreachable", async () => {
		const worker = await startHost("h_worker", "worker-secret");
		const h = await harness();
		await post(h, "/hub/hosts", { label: "worker-1", url: worker.url, password: "worker-secret" });
		const monitor = createHostsMonitor({ store: h.store, hubConfigPath, log: () => {}, intervalMs: 10 });
		const statusAfterRounds = async (rounds: number) => {
			const start = worker.seen.filter((r) => r.path === "/api/v1/identity").length;
			await expect.poll(() => worker.seen.filter((r) => r.path === "/api/v1/identity").length - start, { timeout: 2000 }).toBeGreaterThanOrEqual(rounds);
			// Let the round that just probed record its result.
			await new Promise((resolve) => setTimeout(resolve, 20));
			return (await list(h)).body.hosts[0].status;
		};
		monitor.start();
		try {
			expect(await statusAfterRounds(1)).toBe("online");
			worker.password = "rotated";
			expect(await statusAfterRounds(2)).toBe("auth_failed");
			await worker.close();
			hosts = hosts.filter((x) => x !== worker);
			await expect.poll(async () => (await list(h)).body.hosts[0].status, { timeout: 2000 }).toBe("unreachable");
		} finally {
			monitor.stop();
		}
	});
});
