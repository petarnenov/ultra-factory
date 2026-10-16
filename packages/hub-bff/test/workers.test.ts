import { mkdtempSync, mkdirSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import path from "node:path";
import request from "supertest";
import { afterEach, beforeAll, describe, expect, it, vi } from "vitest";
import { HostsStore } from "../src/hosts-store.ts";
import type { WorkerProvider, WorkerSpec } from "../src/railway-client.ts";
import { createBff } from "../src/server.ts";
import { createWorkersApi } from "../src/workers-api.ts";
import { hashPassword } from "../src/password.ts";

const operatorPassword = "operator-password";
const origin = "http://hub.test";
let passwordHash: string;
let dir: string;
let servers: ReturnType<typeof createBff>[] = [];

beforeAll(async () => { passwordHash = await hashPassword(operatorPassword, { ln: 10, r: 8, p: 1 }); });
afterEach(async () => {
	for (const server of servers) await new Promise<void>((resolve) => server.close(() => resolve()));
	servers = [];
	if (dir) rmSync(dir, { recursive: true, force: true });
});

function fakeProvider(overrides: Partial<WorkerProvider> = {}): WorkerProvider & { specs: WorkerSpec[] } {
	const specs: WorkerSpec[] = [];
	return {
		specs,
		create: vi.fn(async (spec: WorkerSpec) => { specs.push(spec); return { serviceId: "service-1", volumeId: "volume-1", environmentId: "environment-1", url: "https://worker.up.railway.app" }; }),
		getStatus: vi.fn(async () => "running" as const),
		setVariables: vi.fn(async () => {}),
		redeploy: vi.fn(async () => {}),
		delete: vi.fn(async () => {}),
		...overrides,
	};
}

async function setup(provider: WorkerProvider, extras: Partial<Parameters<typeof createWorkersApi>[0]> = {}) {
	dir = mkdtempSync(path.join(tmpdir(), "hub-workers-"));
	const spa = path.join(dir, "spa");
	mkdirSync(spa);
	writeFileSync(path.join(spa, "index.html"), "ok");
	const store = await HostsStore.open(path.join(dir, "data"));
	const workersApi = createWorkersApi({ store, provider, image: "example/worker:latest", log: () => {}, probe: async () => ({ ok: true, identity: { hostId: "worker-host" } }), ...extras });
	const server = createBff({ spaDir: spa, passwordHash, log: () => {}, workersApi, verify: async (candidate) => candidate === operatorPassword });
	servers.push(server);
	const login = await request(server).post("/login").set("Host", "hub.test").set("Origin", origin).send({ password: operatorPassword });
	const cookie = (login.headers["set-cookie"] as unknown as string[])[0].split(";")[0];
	return { server, store, cookie };
}

function createWorker(server: ReturnType<typeof createBff>, cookie: string, body = { label: "worker-a", env: { GH_TOKEN: "gh-secret", AO_GIT_USER_NAME: "Agent", AO_GIT_USER_EMAIL: "agent@example.com" } }) {
	return request(server).post("/hub/workers").set("Host", "hub.test").set("Origin", origin).set("Cookie", cookie).send(body);
}

async function provisioned(server: ReturnType<typeof createBff>, cookie: string, store: HostsStore) {
	const response = await createWorker(server, cookie);
	const id = response.body.worker.id as string;
	await vi.waitFor(() => expect(store.getPublic(id)?.status).toBe("online"));
	return id;
}

function deleteWorker(server: ReturnType<typeof createBff>, cookie: string, id: string, force = false) {
	return request(server).delete(`/hub/workers/${id}${force ? "?force=1" : ""}`).set("Host", "hub.test").set("Origin", origin).set("Cookie", cookie);
}

describe("Railway worker lifecycle", () => {
	it("writes a provisioning record before create and reaches online with a 32-character password", async () => {
		const provider = fakeProvider();
		const h = await setup(provider);
		const response = await createWorker(h.server, h.cookie);
		expect(response.status).toBe(202);
		const id = response.body.worker.id as string;
		await vi.waitFor(() => expect(h.store.getPublic(id)?.status).toBe("online"));
		const saved = h.store.get(id)!;
		expect(saved).toMatchObject({ hostId: "worker-host", status: "online", desired: "running", railway: { serviceId: "service-1", volumeId: "volume-1", environmentId: "environment-1" } });
		expect(h.store.list()[0]?.id).toBe(id);
		await h.store.recordProbe(id, { status: "unreachable" });
		expect(h.store.getPublic(id)).toMatchObject({ id, status: "unreachable" });
		expect(provider.specs[0]?.env.AO_REMOTE_PASSWORD).toHaveLength(32);
		expect(response.text).not.toContain("gh-secret");
		expect(saved.password).not.toBe("gh-secret");
	});

	it("retains a failed record after Railway create errors without leaking error text", async () => {
		const provider = fakeProvider({ create: vi.fn(async () => { throw new Error("Not Authorized secret-value"); }) });
		const h = await setup(provider);
		const response = await createWorker(h.server, h.cookie);
		const id = response.body.worker.id as string;
		await vi.waitFor(() => expect(h.store.getPublic(id)?.status).toBe("failed"));
		expect(h.store.get(id)?.lastError).toBe("Railway authorization failed");
		expect(JSON.stringify(h.store.list())).not.toContain("secret-value");
	});

	it("marks a running deployment failed when the identity probe times out", async () => {
		let now = 0;
		const provider = fakeProvider();
		const h = await setup(provider, { now: () => now, sleep: async (ms) => { now += ms; }, probe: async () => ({ ok: false }) });
		const response = await createWorker(h.server, h.cookie);
		const id = response.body.worker.id as string;
		await vi.waitFor(() => expect(h.store.getPublic(id)?.status).toBe("failed"));
		expect(h.store.get(id)?.lastError).toContain("10 minutes");
		expect(h.store.get(id)?.railway?.serviceId).toBe("service-1");
	});

	it("deletes when the session list is empty", async () => {
		const provider = fakeProvider();
		const h = await setup(provider, { activeSessions: async () => [] });
		const id = await provisioned(h.server, h.cookie, h.store);
		expect((await deleteWorker(h.server, h.cookie, id)).status).toBe(204);
		expect(h.store.getPublic(id)).toBeUndefined();
		expect(provider.delete).toHaveBeenCalledWith("service-1", "volume-1");
	});

	it("deletes when all listed sessions are terminated", async () => {
		const provider = fakeProvider();
		const h = await setup(provider, { activeSessions: async () => [{ id: "done", isTerminated: true, status: "terminated" }] });
		const id = await provisioned(h.server, h.cookie, h.store);
		expect((await deleteWorker(h.server, h.cookie, id)).status).toBe(204);
		expect(h.store.getPublic(id)).toBeUndefined();
	});

	it.each(["idle", "waiting_input"])("blocks deletion for a non-terminated %s session and returns only safe fields", async (state) => {
		const provider = fakeProvider();
		const h = await setup(provider, { activeSessions: async () => [{ id: "session-1", displayName: "Worker task", isTerminated: false, activity: { state }, secret: "must-not-return" }] });
		const id = await provisioned(h.server, h.cookie, h.store);
		const blocked = await deleteWorker(h.server, h.cookie, id);
		expect(blocked.status).toBe(409);
		expect(blocked.body).toEqual({ error: "active_sessions", sessions: [{ id: "session-1", displayName: "Worker task", activity: { state } }] });
		expect(provider.delete).not.toHaveBeenCalled();
	});

	it("fails closed when the worker session list cannot be fetched", async () => {
		const provider = fakeProvider();
		const h = await setup(provider, { activeSessions: async () => { throw new Error("unreachable"); } });
		const id = await provisioned(h.server, h.cookie, h.store);
		const blocked = await deleteWorker(h.server, h.cookie, id);
		expect(blocked.status).toBe(409);
		expect(blocked.body).toEqual({ error: "session_check_failed" });
		expect(provider.delete).not.toHaveBeenCalled();
	});

	it("allows force deletion when the session list cannot be fetched", async () => {
		const provider = fakeProvider();
		const h = await setup(provider, { activeSessions: async () => { throw new Error("unreachable"); } });
		const id = await provisioned(h.server, h.cookie, h.store);
		expect((await deleteWorker(h.server, h.cookie, id, true)).status).toBe(204);
		expect(provider.delete).toHaveBeenCalledWith("service-1", "volume-1");
	});

	it("retains a failed record when Railway denies deletion", async () => {
		const provider = fakeProvider({ delete: vi.fn(async () => { throw new Error("denied"); }) });
		const h = await setup(provider, { activeSessions: async () => [] });
		const id = await provisioned(h.server, h.cookie, h.store);
		const failed = await deleteWorker(h.server, h.cookie, id);
		expect(failed.status).toBe(502);
		expect(h.store.getPublic(id)).toMatchObject({ status: "failed", desired: "removed", lastError: expect.any(String) });
	});
});
